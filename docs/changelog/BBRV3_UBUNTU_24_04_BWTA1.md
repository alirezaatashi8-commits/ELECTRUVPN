# BWTA1 — guarded BBRv3 kernel workflow

- Added `electruvpn-bbrv3-kernel-manager` for Ubuntu 24.04 preflight, status, checksum-validated local kernel bundle staging, post-reboot confirmation, and pending-state cancellation.
- Kernel changes require explicit `--confirm-kernel-change`; the manager does not download kernels, alter GRUB default, or reboot automatically.
- Kept automatic install-time network tuning limited to the running kernel's available BBR implementation.
- Added admin instructions and limitations in `docs/BBRV3_UBUNTU_24_04.md`.
- A successful local checksum is not a provider signature; administrators must independently verify the provider and package provenance.
