# BBRv3 kernel workflow — Ubuntu 24.04 / BETA1

## Design policy

ELECTRUVPN automatically applies only conservative settings supported by the currently running kernel. It does **not** silently replace the kernel. Linux commonly reports BBRv1 and BBRv3 using the same congestion-control name (`bbr`), so `sysctl` output alone is not proof of BBRv3. Google's BBRv3 release is a kernel-source/testing workflow, not a sysctl toggle. See the [official Google BBR repository](https://github.com/google/bbr) and its [v3 build instructions](https://github.com/google/bbr/blob/v3/README.md).

## Supported workflow

The manager is installed as `/usr/local/bin/electruvpn-bbrv3-kernel-manager`. It is a guarded package-staging workflow for Ubuntu 24.04 LTS on amd64/arm64 with GRUB. It does not select a kernel provider and does not download third-party kernels. An administrator must independently choose and verify a provider and supply a local `.deb` bundle. The bundle must include `SHA256SUMS`, every `.deb` must be listed in that checksum file, packages must be kernel image/modules/headers/tools packages for the host architecture, and the image package name/version and expected release must explicitly contain `bbrv3`.

### 1. Inspect current state

```bash
sudo electruvpn-bbrv3-kernel-manager status
sudo electruvpn-bbrv3-kernel-manager preflight
```

### 2. Stage a kernel package bundle (explicit and potentially disruptive)

Put only the trusted provider's kernel `.deb` packages and a matching `SHA256SUMS` in one directory. Independently verify the provider signature/checksum and release notes; SHA256SUMS only protects the local bundle against accidental changes unless the checksum itself came through a trusted channel. Confirm provider compatibility with Ubuntu 24.04 and your VPS boot method. Then, during a maintenance window, run: 

```bash
sudo electruvpn-bbrv3-kernel-manager install-bundle /path/to/kernel-bundle 6.x.y-bbrv3 https://trusted-provider.example/releases --confirm-kernel-change
```

Replace the example release and URL with the exact kernel release and provider you verified. The expected release must match the installed `/boot/vmlinuz-<release>` filename. The tool refuses Secure Boot enabled systems, package transactions that propose removals, unsupported OS/architecture, and bundles without explicit checksums. It keeps the current kernel installed, does not change GRUB's default selection, and does not reboot.

**Do not run the example command verbatim.** The URL and release are placeholders. Do not use a kernel bundle from an unknown source on a production VPN server. Ensure you have out-of-band provider console access and a tested recovery path before staging a custom kernel.

### 3. Reboot manually and confirm

Schedule a maintenance window, reboot manually using your normal provider procedure, and verify that SSH and the panel return. Then run:

```bash
sudo electruvpn-bbrv3-kernel-manager confirm
sudo electruvpn-bbrv3-kernel-manager status
```

Confirmation only succeeds when the running kernel exactly matches the staged release, the kernel advertises `bbr`, and the current TCP congestion control is `bbr`. The resulting marker means the administrator confirmed the provider bundle and booted kernel; it is **not independent cryptographic proof** of the implementation. BBRv1 and BBRv3 share the same sysctl algorithm name.

### Cancel a pending marker

If you have not booted the candidate kernel, `sudo electruvpn-bbrv3-kernel-manager cancel-pending` removes the pending state only. It intentionally leaves kernel packages installed. Do not purge a kernel until you have booted and verified the previous kernel and have a recovery console.

## Limitations

- Kernel packages are not bundled with ELECTRUVPN; there is no single official Ubuntu BBRv3 `.deb` source guaranteed compatible with every VPS.
- This manager does not build a kernel from source, automatically pick a third-party provider, sign kernels, alter Secure Boot, change GRUB defaults, or reboot.
- A custom kernel may lack distribution security updates or provider-specific drivers. Validate security updates, networking drivers, initramfs and bootloader behavior with the kernel provider before use.
- The regular Network Optimizer remains independent: it can enable the BBR implementation present in the current kernel, but that is not the same as installing BBRv3.
