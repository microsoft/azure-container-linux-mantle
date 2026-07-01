// Copyright 2018 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package azure

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v5"
)

var (
	singleVirtualNetworkPrefix = "10.0.0.0/16"
	virtualNetworkPrefix       = []*string{&singleVirtualNetworkPrefix}
	subnetPrefix               = "10.0.0.0/24"
	kolaSubnet                 = "kola-subnet"
	kolaVnet                   = "kola-vn"
)

// findVnetSubnet resolves a pre-existing subnet from a vnet/subnet specifier.
//
// The specifier may be either:
//
//   - a fully-qualified Azure resource ID of a subnet or virtual network, e.g.
//     "/subscriptions/<sub>/resourceGroups/<rg>/providers/Microsoft.Network/virtualNetworks/<vnet>/subnets/<subnet>";
//   - a "<vnet>/<subnet>" name pair (a bare "<vnet>" implies the "default" subnet).
//
// When only a name is given, the lookup is scoped to a.Opts.VnetResourceGroup
// when set; otherwise every virtual network in the subscription is searched by
// name. A name-only search is ambiguous when multiple resource groups contain a
// virtual network with the same name (e.g. parallel dev/prod environments that
// reuse names and CIDRs), so rather than silently picking one — which can place
// instances in a network the caller cannot reach — an error is returned listing
// the candidates. Pass a resource group or a fully-qualified resource ID to
// disambiguate.
func (a *API) findVnetSubnet(vnetSubnetStr string) (Network, error) {
	// A fully-qualified resource ID is unambiguous; use it directly. ARM
	// resource IDs are case-insensitive, so match the prefix that way too.
	if strings.HasPrefix(strings.ToLower(vnetSubnetStr), "/subscriptions/") {
		id, err := arm.ParseResourceID(vnetSubnetStr)
		if err != nil {
			return Network{}, fmt.Errorf("parsing vnet/subnet resource ID %q: %w", vnetSubnetStr, err)
		}
		// The network client is bound to a.subID, so only the resource group and
		// names from the ID are honored — the subscription segment is not. Reject
		// an ID for a different subscription rather than silently resolving the
		// same names in a.subID (the exact wrong-network class this guards against).
		if a.subID != "" && id.SubscriptionID != "" && !strings.EqualFold(id.SubscriptionID, a.subID) {
			return Network{}, fmt.Errorf("vnet/subnet resource ID %q is in subscription %s, but kola is configured for subscription %s", vnetSubnetStr, id.SubscriptionID, a.subID)
		}
		switch {
		case strings.EqualFold(id.ResourceType.String(), "Microsoft.Network/virtualNetworks/subnets"):
			if id.Parent == nil {
				return Network{}, fmt.Errorf("subnet resource ID %q has no parent virtual network", vnetSubnetStr)
			}
			return a.getVnetSubnet(id.ResourceGroupName, id.Parent.Name, id.Name)
		case strings.EqualFold(id.ResourceType.String(), "Microsoft.Network/virtualNetworks"):
			return a.getVnetSubnet(id.ResourceGroupName, id.Name, "default")
		default:
			return Network{}, fmt.Errorf("resource ID %q is not a virtual network or subnet", vnetSubnetStr)
		}
	}

	parts := strings.SplitN(vnetSubnetStr, "/", 2)
	vnetName := parts[0]
	subnetName := "default"
	if len(parts) > 1 {
		subnetName = parts[1]
	}

	// A resource-group-scoped lookup is deterministic.
	if a.Opts.VnetResourceGroup != "" {
		return a.getVnetSubnet(a.Opts.VnetResourceGroup, vnetName, subnetName)
	}

	// Otherwise search the whole subscription by name, failing if the name is
	// ambiguous across resource groups.
	var matches []*armnetwork.VirtualNetwork
	pager := a.netClient.NewListAllPager(nil)
	for pager.More() {
		page, err := pager.NextPage(context.TODO())
		if err != nil {
			return Network{}, fmt.Errorf("failed to iterate vnets: %w", err)
		}
		for _, vnet := range page.Value {
			if vnet != nil && vnet.Name != nil && *vnet.Name == vnetName {
				matches = append(matches, vnet)
			}
		}
	}
	switch len(matches) {
	case 0:
		return Network{}, fmt.Errorf("failed to find vnet %s", vnetName)
	case 1:
		return subnetFromVnet(matches[0], subnetName)
	default:
		ids := make([]string, 0, len(matches))
		for _, m := range matches {
			if m.ID != nil {
				ids = append(ids, *m.ID)
			}
		}
		return Network{}, fmt.Errorf("found %d virtual networks named %q in the subscription; "+
			"pass --azure-vnet-resource-group or a fully-qualified resource ID to disambiguate (candidates: %s)",
			len(matches), vnetName, strings.Join(ids, ", "))
	}
}

// getVnetSubnet fetches a named subnet from a virtual network in a known
// resource group.
func (a *API) getVnetSubnet(resourceGroup, vnetName, subnetName string) (Network, error) {
	resp, err := a.netClient.Get(context.TODO(), resourceGroup, vnetName, nil)
	if err != nil {
		return Network{}, fmt.Errorf("failed to get vnet %s in resource group %s: %w", vnetName, resourceGroup, err)
	}
	return subnetFromVnet(&resp.VirtualNetwork, subnetName)
}

// subnetFromVnet returns the named subnet from an already-resolved virtual network.
func subnetFromVnet(vnet *armnetwork.VirtualNetwork, subnetName string) (Network, error) {
	vnetName := "<unknown>"
	if vnet != nil && vnet.Name != nil {
		vnetName = *vnet.Name
	}
	if vnet == nil || vnet.Properties == nil || vnet.Properties.Subnets == nil {
		return Network{}, fmt.Errorf("failed to find subnet %s in vnet %s", subnetName, vnetName)
	}
	for _, subnet := range vnet.Properties.Subnets {
		if subnet != nil && subnet.Name != nil && *subnet.Name == subnetName {
			return Network{*subnet}, nil
		}
	}
	return Network{}, fmt.Errorf("failed to find subnet %s in vnet %s", subnetName, vnetName)
}

func (a *API) PrepareNetworkResources(resourceGroup string) (Network, error) {
	if a.Opts.VnetSubnetName != "" {
		return a.findVnetSubnet(a.Opts.VnetSubnetName)
	}

	if err := a.createVirtualNetwork(resourceGroup); err != nil {
		return Network{}, err
	}

	subnet, err := a.createSubnet(resourceGroup)
	if err != nil {
		return Network{}, err
	}
	return Network{subnet}, nil
}

func (a *API) createVirtualNetwork(resourceGroup string) error {
	plog.Infof("Creating VirtualNetwork %s", kolaVnet)
	poller, err := a.netClient.BeginCreateOrUpdate(context.TODO(), resourceGroup, kolaVnet, armnetwork.VirtualNetwork{
		Location: &a.Opts.Location,
		Properties: &armnetwork.VirtualNetworkPropertiesFormat{
			AddressSpace: &armnetwork.AddressSpace{
				AddressPrefixes: virtualNetworkPrefix,
			},
		},
	}, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(context.TODO(), nil)
	if err != nil {
		return err
	}
	return nil
}

func (a *API) createSubnet(resourceGroup string) (armnetwork.Subnet, error) {
	plog.Infof("Creating Subnet %s", kolaSubnet)
	poller, err := a.subClient.BeginCreateOrUpdate(context.TODO(), resourceGroup, kolaVnet, kolaSubnet, armnetwork.Subnet{
		Properties: &armnetwork.SubnetPropertiesFormat{
			AddressPrefix: &subnetPrefix,
		},
	}, nil)
	if err != nil {
		return armnetwork.Subnet{}, err
	}
	r, err := poller.PollUntilDone(context.TODO(), nil)
	if err != nil {
		return armnetwork.Subnet{}, err
	}
	return r.Subnet, nil
}

func (a *API) getSubnet(resourceGroup, vnet, subnet string) (armnetwork.Subnet, error) {
	r, err := a.subClient.Get(context.TODO(), resourceGroup, vnet, subnet, nil)
	return r.Subnet, err
}

func (a *API) createPublicIP(resourceGroup string) (*armnetwork.PublicIPAddress, error) {
	name := randomName("ip")
	plog.Infof("Creating PublicIP %s", name)

	poller, err := a.ipClient.BeginCreateOrUpdate(context.TODO(), resourceGroup, name, armnetwork.PublicIPAddress{
		Location: &a.Opts.Location,
		Properties: &armnetwork.PublicIPAddressPropertiesFormat{
			DeleteOption: to.Ptr(armnetwork.DeleteOptionsDelete),
		},
	}, nil)
	if err != nil {
		return nil, err
	}
	r, err := poller.PollUntilDone(context.TODO(), nil)
	if err != nil {
		return nil, err
	}
	ip := r.PublicIPAddress
	ip.Properties.DeleteOption = to.Ptr(armnetwork.DeleteOptionsDelete)
	return &ip, nil
}

func (a *API) getPublicIP(name, resourceGroup string) (string, error) {
	ip, err := a.ipClient.Get(context.TODO(), resourceGroup, name, nil)
	if err != nil {
		return "", err
	}

	if ip.Properties.IPAddress == nil {
		return "", fmt.Errorf("IP Address is nil")
	}

	return *ip.Properties.IPAddress, nil
}

// returns PublicIP, PrivateIP, error
func (a *API) GetIPAddresses(name, publicIPName, resourceGroup string) (string, string, error) {
	privateIP, err := a.GetPrivateIP(name, resourceGroup)
	if err != nil {
		return "", "", err
	}
	if publicIPName == "" {
		return privateIP, privateIP, nil
	}

	publicIP, err := a.getPublicIP(publicIPName, resourceGroup)
	if err != nil {
		return "", "", err
	}
	return publicIP, privateIP, nil
}

func (a *API) GetPrivateIP(name, resourceGroup string) (string, error) {
	nic, err := a.intClient.Get(context.TODO(), resourceGroup, name, nil)
	if err != nil {
		return "", err
	}
	var privateIP *string
	for _, conf := range nic.Properties.IPConfigurations {
		if conf == nil || conf.Properties == nil || conf.Properties.PrivateIPAddress == nil {
			//return "", "", fmt.Errorf("PrivateIPAddress is nil")
			continue
		}
		privateIP = conf.Properties.PrivateIPAddress
		break
	}
	if privateIP == nil {
		return "", fmt.Errorf("no ip configurations found")
	}
	return *privateIP, nil
}

func (a *API) createNIC(ip *armnetwork.PublicIPAddress, subnet *armnetwork.Subnet, resourceGroup string) (*armnetwork.Interface, error) {
	name := randomName("nic")
	ipconf := randomName("nic-ipconf")
	plog.Infof("Creating NIC %s", name)

	poller, err := a.intClient.BeginCreateOrUpdate(context.TODO(), resourceGroup, name, armnetwork.Interface{
		Location: &a.Opts.Location,
		Properties: &armnetwork.InterfacePropertiesFormat{
			IPConfigurations: []*armnetwork.InterfaceIPConfiguration{
				{
					Name: &ipconf,
					Properties: &armnetwork.InterfaceIPConfigurationPropertiesFormat{
						PublicIPAddress:           ip,
						PrivateIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodDynamic),
						Subnet:                    subnet,
					},
				},
			},
			EnableAcceleratedNetworking: to.Ptr(true),
		},
	}, nil)
	if err != nil {
		return nil, err
	}
	r, err := poller.PollUntilDone(context.TODO(), nil)
	if err != nil {
		return nil, err
	}
	return &r.Interface, nil
}
