<h1>
  <img src="https://raw.githubusercontent.com/microsoft/azurelinux/refs/tags/3.0.20260517-3.0/assets/azurelinux-logo-48.png" alt="Azure Container Linux logo" align="left" height="44" /> &nbsp;
Mantle: ACL Test and Release Tooling
</h1>

Mantle is the test and release tooling for [Azure Container Linux (ACL)](https://aka.ms/azurecontainerlinux). It is a fork of the [Flatcar Container Linux Mantle project](https://github.com/flatcar/mantle), extended with ACL-specific test cases, platform support, and CI integration.

## Overview
Mantle is composed of many utilities:
 - `cork` for handling the Container Linux SDK
 - `gangue` for downloading from Google Storage
 - `kola` for launching instances and running tests
 - `kolet` an agent for kola that runs on instances
 - `ore` for interfacing with cloud providers
 - `plume` for releasing Container Linux

All of the utilities support the `help` command to get a full listing of their subcommands
and options.

## Tools

### cork
Cork is a now-deprecated tool that was used to help in working with Container Linux images and the SDK.

Please see [developer guides](https://www.flatcar.org/docs/latest/reference/developer-guides/) to see how to work with Flatcar SDK.

### gangue
Gangue is a tool for downloading and verifying files from Google Storage with authenticated requests.
It is primarily used by the SDK.

#### gangue get
Get a file from Google Storage and verify it using GPG.

### kola
Kola is a framework for testing software integration in Container Linux instances
across multiple platforms. It is primarily designed to operate within
the Container Linux SDK for testing software that has landed in the OS image.
Ideally, all software needed for a test should be included by building
it into the image from the SDK.

Kola supports running tests on multiple platforms, currently QEMU, GCE,
AWS, VMware VSphere, Packet, and OpenStack. In the future systemd-nspawn and other
platforms may be added.
Machines on cloud platforms do not have direct access to the kola so tests may depend on
Internet services such as discovery.etcd.io or quay.io instead.

Kola outputs assorted logs and test data to `_kola_temp` for later
inspection.

Kola is still under heavy development and it is expected that its
interface will continue to change.

By default, kola uses the `qemu` platform with the image
`/mnt/host/source/src/build/images/BOARD/latest/flatcar_production_image.bin`.

## Core Commands

Kola provides several key commands for testing and development:

- **`kola run`** - Execute specific tests on various platforms
- **`kola list`** - List all available tests  
- **`kola spawn`** - Launch interactive instances for debugging

For complete command documentation, options, and examples, see [kola/README.md](kola/README.md).

#### Manhole
The `platform.Manhole()` function creates an interactive SSH session which can
be used to inspect a machine during a test.

### kolet
kolet is run on kola instances to run native functions in tests. Generally kolet
is not invoked manually.

### ore
Ore provides a low-level interface for each cloud provider. It has commands
related to launching instances on a variety of platforms (gcloud, aws,
azure, esx, and packet) within the latest SDK image. Ore mimics the underlying
api for each cloud provider closely, so the interface for each cloud provider
is different. See each providers `help` command for the available actions.

Note, when uploading to some cloud providers (e.g. gce) the image may need to be packaged
with a different --format (e.g. --format=gce) when running `image_to_vm.sh`

### plume
Plume is the Container Linux release utility. Releases are done in two stages,
each with their own command: pre-release and release. Both of these commands are idempotent.

#### plume pre-release
The pre-release command does as much of the release process as possible without making anything public.
This includes uploading images to cloud providers (except those like gce which don't allow us to upload
images without making them public).

### plume release
Publish a new Container Linux release. This makes the images uploaded by pre-release public and uploads
images that pre-release could not. It copies the release artifacts to public storage buckets and updates
the directory index.

#### plume index
Generate and upload index.html objects to turn a Google Cloud Storage
bucket into a publicly browsable file tree. Useful if you want something
like Apache's directory index for your software download repository.
Plume release handles this as well, so it does not need to be run as part of
the release process.

## Platform Credentials
Each platform reads the credentials it uses from different files. The `aws`, `azure`, `do`, `esx` and `packet`
platforms support selecting from multiple configured credentials, call "profiles". The examples below
are for the "default" profile, but other profiles can be specified in the credentials files and selected
via the `--<platform-name>-profile` flag:
```
kola spawn -p aws --aws-profile other_profile
```

### aws
`aws` reads the `~/.aws/credentials` file used by Amazon's aws command-line tool.
It can be created using the `aws` command:
```
$ aws configure
```
To configure a different profile, use the `--profile` flag
```
$ aws configure --profile other_profile
```

The `~/.aws/credentials` file can also be populated manually:
```
[default]
aws_access_key_id = ACCESS_KEY_ID_HERE
aws_secret_access_key = SECRET_ACCESS_KEY_HERE
```

To install the `aws` command in the SDK, run:
```
sudo emerge --ask awscli
```

### azure
`azure` uses `~/.azure/azureProfile.json`. This can be created using the `az` [command](https://docs.microsoft.com/en-us/cli/azure/install-azure-cli):
```
$ az login`
```
It also requires that the environment variable `AZURE_AUTH_LOCATION` points to a JSON file (this can also be set via the `--azure-auth` parameter). The JSON file will require a service provider active directory account to be created.

Service provider accounts can be created via the `az` command (the output will contain an `appId` field which is used as the `clientId` variable in the `AZURE_AUTH_LOCATION` JSON):
```
az ad sp create-for-rbac
```

The client secret can be created inside of the Azure portal when looking at the service provider account under the `Azure Active Directory` service on the `App registrations` tab.

You can find your subscriptionId & tenantId in the `~/.azure/azureProfile.json` via:
```
cat ~/.azure/azureProfile.json | jq '{subscriptionId: .subscriptions[].id, tenantId: .subscriptions[].tenantId}'
```

The JSON file exported to the variable `AZURE_AUTH_LOCATION` should be generated by hand and have the following contents:
```
{
  "clientId": "<service provider id>", 
  "clientSecret": "<service provider secret>", 
  "subscriptionId": "<subscription id>", 
  "tenantId": "<tenant id>", 
  "activeDirectoryEndpointUrl": "https://login.microsoftonline.com", 
  "resourceManagerEndpointUrl": "https://management.azure.com/", 
  "activeDirectoryGraphResourceId": "https://graph.windows.net/", 
  "sqlManagementEndpointUrl": "https://management.core.windows.net:8443/", 
  "galleryEndpointUrl": "https://gallery.azure.com/", 
  "managementEndpointUrl": "https://management.core.windows.net/"
}

```

### do
`do` uses `~/.config/digitalocean.json`. This can be configured manually:
```
{
    "default": {
        "token": "token goes here"
    }
}
```

### esx
`esx` uses `~/.config/esx.json`. This can be configured manually:
```
{
    "default": {
        "server": "server.address.goes.here",
        "user": "user.goes.here",
        "password": "password.goes.here"
    }
}
```

### gce
`gce` uses the `~/.boto` file. When the `gce` platform is first used, it will print
a link that can be used to log into your account with gce and get a verification code
you can paste in. This will populate the `.boto` file.

See [Google Cloud Platform's Documentation](https://cloud.google.com/storage/docs/boto-gsutil)
for more information about the `.boto` file.

### openstack
`openstack` uses `~/.config/openstack.json`. This can be configured manually:
```
{
    "default": {
        "auth_url": "auth url here",
        "tenant_id": "tenant id here",
        "tenant_name": "tenant name here",
        "username": "username here",
        "password": "password here",
        "user_domain": "domain id here",
        "floating_ip_pool": "floating ip pool here",
        "region_name": "region here"
    }
}
```

`user_domain` is required on some newer versions of OpenStack using Keystone V3 but is optional on older versions. `floating_ip_pool` and `region_name` can be optionally specified here to be used as a default if not specified on the command line.

### packet
`packet` uses `~/.config/packet.json`. This can be configured manually:
```
{
	"default": {
		"api_key": "your api key here",
		"project": "project id here"
	}
}
```

### qemu
`qemu` is run locally and needs no credentials, but does need to be run as root.

### qemu-unpriv
`qemu-unpriv` is run locally and needs no credentials. It has a restricted set of functionality compared to the `qemu` platform, such as:

- Single node only, no machine to machine networking
- DHCP provides no data (forces several tests to be disabled)
- No [Local cluster](platform/local/)

## Engagement & support

- **Bugs and feature requests:** file a [GitHub issue](https://github.com/microsoft/azure-container-linux/issues).
- **Security vulnerabilities:** do **not** open a public issue. Follow the process in [SECURITY.md](SECURITY.md).
- **Pull requests:** see [CONTRIBUTING.md](CONTRIBUTING.md) for workflow and conventions.

This project has adopted the [Microsoft Open Source Code of Conduct](https://opensource.microsoft.com/codeofconduct/). For more information, see the [Code of Conduct FAQ](https://opensource.microsoft.com/codeofconduct/faq/) or contact [opencode@microsoft.com](mailto:opencode@microsoft.com) with any additional questions or comments.

## Trademarks

This project may contain trademarks or logos for projects, products, or services. Authorized use of Microsoft trademarks or logos is subject to and must follow [Microsoft's Trademark & Brand Guidelines](https://www.microsoft.com/en-us/legal/intellectualproperty/trademarks/usage/general). Use of Microsoft trademarks or logos in modified versions of this project must not cause confusion or imply Microsoft sponsorship. Any use of third-party trademarks or logos is subject to those third parties' policies.

## Acknowledgments

Any Linux distribution, including Azure Container Linux, benefits from contributions by the open-source software community. We gratefully acknowledge all contributions made from the broader community.

We specifically want to thank [the Flatcar Container Linux Project](https://www.flatcar.org/) for providing the original Mantle tooling. We are proud to participate and contribute to this community.

## License

Unless otherwise specified (see below), the content of this repository is distributed under an [MIT license](LICENSE).

This repository contains files derived from the [Flatcar Container Linux](https://www.flatcar.org/) Mantle project, which are licensed under the Apache License 2.0. These files retain their original license and copyright notices.

For convenience, the original Apache 2.0 license text is included in [LICENSE-APACHE-2.0](LICENSE-APACHE-2.0).

"Otherwise specified" is recorded in the source tree via the original upstream per-file license headers (and any accompanying upstream license/copyright notices). When a file includes an Apache 2.0 header (or other explicit license marker), that file is licensed under those terms rather than MIT.
