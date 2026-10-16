package com.gosshd.desktop.local;

public final class ServerAddressTest {
  public static void main(String[] args) {
    accept("https://bastion.example.com/", "https://bastion.example.com");
    accept("https://bastion.example.com:8443", "https://bastion.example.com:8443");
    accept("http://127.0.0.1:18080/", "http://127.0.0.1:18080");
    accept("http://localhost:18080", "http://localhost:18080");
    for (String value : new String[] {"", "bastion.example.com", "http://bastion.example.com",
        "https://user:secret@bastion.example.com", "https://bastion.example.com/path",
        "https://bastion.example.com?token=example", "https://bastion.example.com/#secret",
        "https://bastion.example.com:0", "https://bastion.example.com:65536"}) {
      try { ServerAddress.normalize(value); throw new AssertionError("Accepted invalid address"); }
      catch (IllegalArgumentException expected) { }
    }
    System.out.println("Server address validation passed");
  }
  private static void accept(String input, String expected) {
    if (!ServerAddress.normalize(input).equals(expected)) throw new AssertionError("Incorrect normalization");
  }
}
