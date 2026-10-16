package com.gosshd.desktop.local;

import java.net.URI;

public final class ServerAddress {
  public static String normalize(String value) {
    try {
      if (value.length() > 2048) throw new IllegalArgumentException("Server address too long");
      URI uri = new URI(value.trim());
      String host = uri.getHost();
      String path = uri.getPath();
      boolean loopback = "localhost".equalsIgnoreCase(host) || "127.0.0.1".equals(host)
          || "::1".equals(host) || "[::1]".equals(host);
      if (host == null || host.isEmpty() || uri.getRawUserInfo() != null
          || uri.getRawQuery() != null || uri.getRawFragment() != null
          || (path != null && !path.isEmpty() && !path.equals("/"))
          || uri.getPort() < -1 || uri.getPort() == 0 || uri.getPort() > 65535
          || !("https".equals(uri.getScheme()) || ("http".equals(uri.getScheme()) && loopback)))
        throw new IllegalArgumentException("Invalid server address");
      String server = uri.toASCIIString();
      return server.endsWith("/") ? server.substring(0, server.length() - 1) : server;
    } catch (Exception error) {
      throw new IllegalArgumentException("Use HTTPS, or HTTP on loopback for development");
    }
  }
  private ServerAddress() {}
}
