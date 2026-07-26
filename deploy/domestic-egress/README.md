# Domestic temporary egress

This directory contains secret-free templates for the optional domestic egress. The production application remains on `47.251.87.147`. For Video Collector, the shared domestic host only adds WireGuard and a private, non-caching Squid listener; its existing and future unrelated workloads remain untouched.

## Required input

Do not begin server changes until the domestic server IPv4, Linux distribution, SSH method, bandwidth allowance, UDP availability, cloud security-group rules, existing workloads, listeners, Docker networks, global proxy settings and resource baseline are confirmed.

## Shared-host isolation

- Snapshot addresses, routes, policy rules, listeners, firewall rules, systemd services, Docker networks and resource use before installation.
- Use the dedicated `wg-vc-egress` interface and `squid-video-collector.service` only. Keep Linux interface names within the 15-character limit.
- Install the proxy configuration as `/etc/squid/video-collector.conf`; never overwrite the default Squid configuration or restart an existing `squid.service`.
- Do not change the default route, DNS, global proxy environment, Docker daemon, Nginx or another project's files and services.
- Do not restart Docker, Nginx, SSH, an existing VPN/proxy or an unrelated project service.
- Apply CPU, memory, task and file-descriptor limits after measuring available resources.
- After every change, recheck existing project health, routes, listeners and resource use. Stop and roll back on any regression.

## Network installation order

1. Install WireGuard from each server's stable operating-system repository.
2. Generate each private key on the server that owns it. Keep files mode `0600` and never copy private keys into this repository.
3. Install the matching `wg-*.conf.example` as `/etc/wireguard/wg-vc-egress.conf`, replace placeholders and start `wg-quick@wg-vc-egress`.
4. On the domestic cloud security group, allow UDP `51820` only from `47.251.87.147`. Do not add a public TCP `3128` rule.
5. Install the distribution's maintained Squid package (currently 5.9 on the domestic Ubuntu 22.04 host) without automatically starting its default service. Stop if Squid already exists or TCP `3128` conflicts until the existing owner is identified.
6. Install `squid.conf.example` as `/etc/squid/video-collector.conf` and the dedicated service template as `/etc/systemd/system/squid-video-collector.service`.
7. Run `squid -k parse -f /etc/squid/video-collector.conf`, start only `squid-video-collector.service`, and confirm it listens only on `10.77.0.2:3128`.

## Required checks

From `47.251.87.147`:

```bash
ping -c 3 10.77.0.2
curl --fail --show-error --proxy http://10.77.0.2:3128 https://example.com/ -o /dev/null
curl --fail --show-error --proxy http://10.77.0.2:3128 http://169.254.169.254/
```

The first two commands must succeed. The metadata request must be denied. From any public host, TCP `3128` must be unreachable.

Before enabling the application, run one public, cookie-free domestic platform parse and the smallest complete download through yt-dlp's `--proxy` option from inside the production container. Confirm that the domestic server contains no project directory, task files or media cache.

## Application enable and disable

Only after the network PoC succeeds, set the seven `VIDEO_COLLECTOR_*EGRESS*`/`VIDEO_COLLECTOR_CN_PROXY_*` variables documented in the project deployment guide and restart the existing application container.

To disable the feature without changing the image:

```dotenv
VIDEO_COLLECTOR_EGRESS_MODE=off
```

Restart the container and verify `/health` reports `"egressStatus":"off"`.
