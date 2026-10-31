# BBR automation and BBRv3 reporting

- Installer now attempts conservative BBR + fq + TCP MTU probing automatically when the running kernel exposes BBR. Failure is non-fatal.
- Installs `/usr/local/bin/electruvpn-network-optimizer` for status/apply/rollback operations.
- Panel reports BBRv3 as confirmed only when `/etc/electruvpn/bbrv3-kernel-installed` exists. It intentionally does not infer BBRv3 from `tcp_available_congestion_control`, because both BBRv1 and BBRv3 register as `bbr`.
- This release does not silently replace the Linux kernel. Installing a patched BBRv3 kernel requires a separately validated kernel package, bootloader/initramfs checks, and a planned reboot; doing that automatically can interrupt or permanently lock out a remote server.

Commands:

```sh
electruvpn-network-optimizer status
electruvpn-network-optimizer apply
electruvpn-network-optimizer rollback
```
