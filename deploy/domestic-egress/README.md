# Domestic temporary egress

This directory contains secret-free templates for the optional domestic egress. The production application remains on `47.251.87.147`; the domestic server runs only WireGuard and a private, non-caching Squid listener.

## Required input

Do not begin server changes until the domestic server IPv4, Linux distribution, SSH method, bandwidth allowance, UDP availability and cloud security-group rules are confirmed.

## Network installation order

1. Install WireGuard from each server's stable operating-system repository.
2. Generate each private key on the server that owns it. Keep files mode `0600` and never copy private keys into this repository.
3. Install the matching `wg-*.conf.example` as `/etc/wireguard/wg-video-collector.conf`, replace placeholders and start `wg-quick@wg-video-collector`.
4. On the domestic cloud security group, allow UDP `51820` only from `47.251.87.147`. Do not add a public TCP `3128` rule.
5. Install the distribution's maintained Squid 6/7 package on the domestic server. Check which configuration directives that exact version supports before replacing its configuration.
6. Install `squid.conf.example`, run `squid -k parse`, then restart Squid. Confirm it listens only on `10.77.0.2:3128`.

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
