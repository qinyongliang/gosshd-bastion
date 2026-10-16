package com.gosshd.desktop.lifecycle;

import android.content.Context;
import com.gosshd.desktop.R;
import java.io.BufferedReader;
import java.io.File;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.TimeUnit;
import java.util.function.Consumer;

// The service owns one fixed, package-installed executable. Never execute a
// downloaded binary, Intent command or writable app-data executable.
final class AgentRuntime implements AutoCloseable {
  private final Context context;
  private final File config;
  private final Consumer<Integer> status;
  private volatile boolean stopped;
  private Process child;
  private Thread worker;

  AgentRuntime(Context context, File config, Consumer<Integer> status) {
    this.context = context;
    this.config = config;
    this.status = status;
  }

  void start() {
    worker = new Thread(this::run, "apk-agent-supervisor");
    worker.start();
  }

  private void run() {
    File executable = new File(context.getApplicationInfo().nativeLibraryDir, "libgosshd_agent.so");
    if (!executable.isFile() || !executable.canExecute()) {
      status.accept(R.string.agent_missing_binary);
      return;
    }
    long delay = 1000;
    while (!stopped) {
      Process process = null;
      try {
        status.accept(R.string.agent_connecting);
        ProcessBuilder builder = new ProcessBuilder(executable.getAbsolutePath(), config.getAbsolutePath());
        builder.directory(context.getFilesDir());
        builder.redirectErrorStream(true);
        builder.environment().put("HOME", new File(context.getFilesDir(), "home").getAbsolutePath());
        builder.environment().put("TMPDIR", context.getCacheDir().getAbsolutePath());
        builder.environment().put("SHELL", "/system/bin/sh");
        synchronized (this) {
          if (stopped) return;
          child = process = builder.start();
        }
        // Drain output without storing raw server diagnostics or credentials.
        try (BufferedReader reader = new BufferedReader(new InputStreamReader(process.getInputStream(), StandardCharsets.UTF_8))) {
          StringBuilder line = new StringBuilder();
          boolean overflow = false;
          for (int value; !stopped && (value = reader.read()) != -1;) {
            if (value == '\n') {
              if (!overflow && line.toString().equals("GOSSHD_APK_STATE online")) {
                delay = 1000;
                status.accept(R.string.agent_online);
              } else if (!overflow && line.toString().equals("GOSSHD_APK_STATE offline")) {
                status.accept(R.string.agent_connecting);
              }
              line.setLength(0);
              overflow = false;
            } else if (line.length() < 1024) line.append((char)value);
            else overflow = true;
          }
        }
        process.waitFor();
      } catch (Exception ignored) {
        // Only localized, non-sensitive status is exposed to the UI.
      } finally {
        if (process != null) terminate(process);
        synchronized (this) { child = null; }
      }
      if (stopped) return;
      status.accept(R.string.agent_retrying);
      try { Thread.sleep(delay); } catch (InterruptedException ignored) { return; }
      delay = Math.min(30000, delay * 2);
    }
  }

  private static void terminate(Process process) {
    process.destroy();
    try { if (!process.waitFor(2, TimeUnit.SECONDS)) process.destroyForcibly(); }
    catch (InterruptedException ignored) { process.destroyForcibly(); }
  }

  public synchronized void close() {
    stopped = true;
    if (child != null) {
      Process process = child;
      process.destroy();
      new Thread(() -> terminate(process), "apk-agent-stop").start();
    }
    if (worker != null) worker.interrupt();
  }
}
