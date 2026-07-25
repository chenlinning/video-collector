const PRIVATE_HOST_MESSAGE = "不允许访问本机、局域网或保留网络地址";

function parseIpv4(hostname: string): number[] | null {
  const parts = hostname.split(".");
  if (parts.length !== 4 || parts.some((part) => !/^\d{1,3}$/.test(part))) {
    return null;
  }

  const values = parts.map(Number);
  return values.every((value) => value >= 0 && value <= 255) ? values : null;
}

function isPrivateIpv4(parts: number[]): boolean {
  const [a, b] = parts;
  return (
    a === 0 ||
    a === 10 ||
    a === 127 ||
    (a === 100 && b >= 64 && b <= 127) ||
    (a === 169 && b === 254) ||
    (a === 172 && b >= 16 && b <= 31) ||
    (a === 192 && b === 168) ||
    (a === 198 && (b === 18 || b === 19)) ||
    a >= 224
  );
}

function isPrivateIpv6(hostname: string): boolean {
  const host = hostname.replace(/^\[|\]$/g, "").toLowerCase();
  if (!host.includes(":")) {
    return false;
  }

  if (host === "::" || host === "::1") {
    return true;
  }

  if (/^(fc|fd)/.test(host) || /^fe[89ab]/.test(host)) {
    return true;
  }

  const mappedIpv4 = host.match(/::ffff:(\d+\.\d+\.\d+\.\d+)$/)?.[1];
  return mappedIpv4 ? isPrivateIpv4(parseIpv4(mappedIpv4) ?? []) : false;
}

export function assertPublicMediaUrl(value: string): URL {
  const input = value.trim();
  let url: URL;

  try {
    url = new URL(input);
  } catch {
    throw new Error("请输入有效的视频链接");
  }

  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error("仅支持 HTTP 或 HTTPS 视频链接");
  }

  const hostname = url.hostname.toLowerCase();
  if (
    hostname === "localhost" ||
    hostname.endsWith(".localhost") ||
    hostname.endsWith(".local")
  ) {
    throw new Error(PRIVATE_HOST_MESSAGE);
  }

  const ipv4 = parseIpv4(hostname);
  if ((ipv4 && isPrivateIpv4(ipv4)) || isPrivateIpv6(hostname)) {
    throw new Error(PRIVATE_HOST_MESSAGE);
  }

  if (url.username || url.password) {
    throw new Error("链接中不能包含用户名或密码");
  }

  return url;
}
