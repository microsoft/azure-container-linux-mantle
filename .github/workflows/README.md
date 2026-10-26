# GitHub Actions workflows

The Flatcar-inherited workflows in this directory are disabled by renaming
their `.yml` or `.yaml` files to `.disabled`. They target Flatcar infrastructure
and GHCR packages, or the `main` branch rather than Mantle's `aclmain` branch;
the scheduled image mirroring and Calico sync workflows have also failed in
this repository. GitHub Actions does not recognize these files as workflows,
but their contents remain available if an ACL-specific replacement is needed.

The `.sh` helper scripts are retained unchanged. This does not disable
GitHub Actions repository-wide or affect other CI, Copilot, or Dependabot.
