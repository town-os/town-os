# contrib

Standalone utilities that complement Town OS but are not part of the core system. These scripts are meant to be copied into `~/bin` or another directory on your `PATH`.

## set-dns

Force or reset the DNS server for a network interface. Sets a static DNS server and disables DHCP-provided DNS so the setting is not overwritten on lease renewal. Pass `reset` to restore DHCP-managed DNS.

The script auto-detects which network backend is managing the system (netplan, NetworkManager, systemd-networkd, ConnMan, ifupdown, or bare `/etc/resolv.conf`) and uses the appropriate tool. DHCP DNS is suppressed for all backends that support it, so the configured server is never silently replaced.

### Usage

```
set-dns <interface> <ip|reset>
```

### Examples

```sh
# Point enp7s0 at a local DNS server
set-dns enp7s0 192.168.1.1

# Use an IPv6 address
set-dns wlan0 fd12::1

# Revert to DHCP-provided DNS
set-dns enp7s0 reset
```

### Backend detection order

| Priority | Backend | Typical distros |
|----------|---------|-----------------|
| 1 | netplan | Ubuntu, cloud images |
| 2 | NetworkManager | Arch, Fedora, most desktops |
| 3 | systemd-networkd | Minimal/server installs |
| 4 | ConnMan | Embedded/IoT |
| 5 | ifupdown | Legacy Debian |
| 6 | /etc/resolv.conf | Fallback (non-persistent) |

### Requirements

`ip`, `sudo`, and the CLI tools for the detected backend (`nmcli`, `networkctl`, `netplan`, `connmanctl`, or `ifup`).
