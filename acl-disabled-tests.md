<!-- Copyright (c) Microsoft Corporation. -->
<!-- Licensed under the MIT License. -->

# ACL Disabled Tests

ACL is leveraging the existing CoreOS Mantle test suite to validate the functionality of the Azure Linux image. However, some tests are not applicable in the ACL context and are therefore disabled.

Specifically, ACL test suite has been bootstrapped by running the same set of tests that were marked for `cl`, which is what Flatcar is using.

## Disabled Tests

The following tests are currently disabled in ACL:
|Test Name|Reason|
|---|---|
|cl.locksmith.cluster|A/B update not targeted for GA|
|cl.locksmith.reboot|A/B update not targeted for GA|
|cl.omaha.ping|A/B update not targeted for GA|
|cl.sysext.fallbackdownload|Online registry of sysexts not available for GA|
|cl.tang.nonroot|Encryption not targetted for GA|
|cl.tang.root|Encryption not targetted for GA|
|cl.tpm.eventlog|Encryption not targetted for GA|
|cl.tpm.nonroot|Encryption not targetted for GA|
|cl.tpm.root|Encryption not targetted for GA|
|cl.tpm.root-cryptenroll|Encryption not targetted for GA|
|cl.tpm.root-cryptenrool-pcr-noupdate|Encryption not targetted for GA|
|cl.tpm.root-cryptenrool-pcr-withupdate|Encryption not targetted for GA|
|cl.update.reboot|A/B update not targeted for GA|
|cl.update.badverity|A/B update not targeted for GA|
|coreos.locksmith.tls|A/B update not targeted for GA|
|coreos.update.badusr|A/B update not targeted for GA|
|docker.devicemapper-storage|No longer running against latest upstream.|
|docker.lib-coreos-dockerd-compat|No longer running against latest upstream.|
|docker.network-nmap-ncat|No longer running against latest upstream.|
|docker.network-openbsd-nc|ACL is carrying NMAP ncat, which is sufficient for AKS scenarios.|
|docker.oldclient|No longer running against latest upstream.|
|cl.ignition.kargs|Enabled for grub boot mode only. ACL UKI (systemd-boot) mode does not yet support dynamic kernel argument injection via addons.|
|cl.osreset.ignition-rerun|flatcar-reset uses dynamic kernel argument injection, not yet supported.|
|sysext.custom-oem|Depends on flatcar-reset, which is not currently supported in ACL|


## Modified Tests

The following tests have been renamed and modified for ACL:
|Original Test|ACL Test|Changes|Reason|
|---|---|---|---|
|cl.basic|acl.basic|Removed `UpdateEngineKeys` subtest; `ServicesActive` does not check for `update-engine.service`|A/B update not targeted for GA|
|cl.basic|acl.basic|Removed `Microcode` subtest; incompatible kernel config|AzL does not embed microcode updates into the kernel|
|cl.basic|acl.basic|Removed `SymlinkFlatcar` subtest; removed coreos/flatcar
backcompat symlinks|Attempting to start fresh with ACL|
|cl.filesystem|cl.filesystem|Added /usr/share/distro/etc into deadlink
exceptions|New location for etc lower dir|
|cl.internet|acl.internet|Removed `UpdateEngine` subtest|A/B update not targeted for GA|
|misc.fips|acl.misc.fips|FIPS activated via UKI addon instead of `kernel_arguments`; MD5 assertion skipped|ACL uses UKI boot with SymCrypt, which supports MD5 in FIPS mode|
|cl.network.initramfs.second-boot|acl.network.initramfs.second-boot|Forked as ACL-specific test; excludes `azure` from `ExcludePlatforms`|ACL's SELinux toggle enables networkd in initrd on Azure for all boots; test only valid on QEMU|
|docker.selinux|docker.selinux|Skip legacy version branch on ACL|ACL's on-machine `VERSION=` (`3.0.x`) is `< 3510.4`, causing the wrong AVC regex (`vda` + `svirt_lxc_net_t`)|
|cl.overlay.cleanup|cl.overlay.cleanup|Modify run method `OverlayCleanup` to use modified filesystem paths for testing |ACL's image does not have the test paths because the AzL RPMs providing them are skipped during build.|
