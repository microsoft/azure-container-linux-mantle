# byon test mutations

What each byon-enabled test does to the node. byon targets existing nodes
not provisioned by kola, reusing a node for multiple tests. Tests that
mutate the node minimally are preferred.

Connecting does not mutate the node (`NewMachine` skips `platform.StartMachine`);
only the per-test actions below do.

| Test | Reboots? | Mutations |
|---|---|---|
| `coreos.tls.fetch-urls` | No | None. `curl`/`wget` to external URLs; read-only. |
| `cl.filesystem` | No | None. Read-only `find` scans of the filesystem. |
| `coreos.selinux.boolean` | Yes | Flips an SELinux boolean (`setsebool`) then reverts it; reboots. |
| `coreos.selinux.enforce` | Yes | `setenforce 0`/`1` toggles; edits `/etc/selinux/config` (leaves `config.old`); reboots. |
