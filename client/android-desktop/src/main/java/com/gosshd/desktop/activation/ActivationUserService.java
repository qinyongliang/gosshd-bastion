package com.gosshd.desktop.activation;

import java.io.ByteArrayOutputStream;
import java.util.concurrent.TimeUnit;

// Fixed Shizuku UserService capability: merge only our own accessibility component.
// No general remote execution method, caller command, package or settings key.
public final class ActivationUserService extends IActivation.Stub {
  public ActivationUserService() {}

  private String settings(String... args) throws Exception {
    String[] command = new String[args.length + 1];
    command[0] = "/system/bin/settings";
    System.arraycopy(args, 0, command, 1, args.length);
    Process process = new ProcessBuilder(command).redirectErrorStream(true).start();
    if (!process.waitFor(5, TimeUnit.SECONDS)) {
      process.destroy();
      throw new IllegalStateException("settings timed out");
    }
    ByteArrayOutputStream output = new ByteArrayOutputStream();
    byte[] buffer = new byte[1024];
    int count;
    while ((count = process.getInputStream().read(buffer)) != -1) {
      if (output.size() + count > 16384)
        throw new IllegalStateException("settings output too large");
      output.write(buffer, 0, count);
    }
    if (process.exitValue() != 0)
      throw new IllegalStateException(
          "settings rejected activation (exit " + process.exitValue() + ")");
    return output.toString("UTF-8").trim();
  }

  @Override
  public String activate() {
    try {
      String own = "com.gosshd.desktop/com.gosshd.desktop.access.DesktopAccessibilityService";
      String current = settings("get", "secure", "enabled_accessibility_services");
      settings(
          "put",
          "secure",
          "enabled_accessibility_services",
          AccessibilityActivation.merge(current, own));
      settings("put", "secure", "accessibility_enabled", "1");
      return "Activation requested; accessibility connects asynchronously";
    } catch (Exception e) {
      return "Activation failed: " + e.getClass().getSimpleName() + " " + e.getMessage();
    }
  }

  @Override
  public void destroy() {
    System.exit(0);
  }
}
