## Summary <!-- REQUIRED -->
<!-- Quick explanation of the changes. What does the PR accomplish, why was it needed? -->


## Change Log <!-- REQUIRED -->
<!-- Detail the changes made here. -->
<!-- List any kola tests, platform integrations, or commands affected. -->
- Change
- Change

## Type of Change <!-- REQUIRED -->
<!-- Check all that apply -->
- [ ] New kola test
- [ ] Kola test fix/update
- [ ] Platform integration change (Azure, QEMU, etc.)
- [ ] CLI/command change
- [ ] CI/automation change
- [ ] Bug fix
- [ ] Documentation update

## Associated Issues <!-- optional -->
<!-- Link to GitHub issues if possible. -->
<!-- Use "fixes #xxxx" to auto-close an associated issue once the PR is merged. -->
<!-- - fixes #xxxx -->

## Test Methodology <!-- REQUIRED -->
<!-- How was this validated? e.g. go test, kola run, local execution, pipeline run, etc. -->
- Test details:

<!--
These HTML comments are not rendered in the PR description.
Feel free to delete sections that do not apply to your PR, or add additional details.
-->

## Merge Checklist <!-- REQUIRED -->
<!-- You can set them now ([x]) or set them later using the GitHub UI -->
**All applicable** boxes should be checked before merging
- [ ] `go build ./...` passes
- [ ] Docker build succeeds (if applicable)
- [ ] `go test ./...` passes
- [ ] `go vet ./...` reports no issues
- [ ] Relevant kola tests pass against a test image
- [ ] Documentation has been updated to match any changes
- [ ] Ready to merge
