package com.gosshd.desktop.local;

import android.content.Context;
import android.content.SharedPreferences;
import android.system.Os;
import android.util.AtomicFile;
import java.io.File;
import java.io.FileOutputStream;
import java.nio.charset.StandardCharsets;
import org.json.JSONObject;

public final class AgentSettings {
  private final Context context;
  private final SharedPreferences prefs;

  public AgentSettings(Context context) {
    this.context = context;
    prefs = context.getSharedPreferences("agent-connection", Context.MODE_PRIVATE);
  }
  public String server() { return prefs.getString("server", ""); }
  public String token() { return prefs.getString("enrollment", ""); }
  public boolean configured() { return !server().isEmpty() && !token().isEmpty(); }

  public void save(String server, String token) {
    server = ServerAddress.normalize(server);
    token = token.trim();
    if (token.isEmpty() || token.length() > 4096)
      throw new IllegalArgumentException("Enrollment token required");
    for (int i = 0; i < token.length(); i++)
      if (Character.isISOControl(token.charAt(i))) throw new IllegalArgumentException("Invalid enrollment token");
    if (!prefs.edit().putString("server", server).putString("enrollment", token).commit())
      throw new IllegalStateException("Cannot save connection");
  }

  public String configuration(LocalState state) throws Exception {
    return new JSONObject().put("server", server()).put("enrollment_token", token())
        .put("desktop_enabled", state.enabled()).toString();
  }

  public File writeConfiguration(String json, LocalState state) throws Exception {
    if (state.enabled()) {
      try (FileOutputStream out = context.openFileOutput("desktop-pairing.token", Context.MODE_PRIVATE)) {
        out.write(state.token().getBytes(StandardCharsets.UTF_8));
      }
    }
    File path = new File(context.getFilesDir(), "agent-config.json");
    AtomicFile atomic = new AtomicFile(path);
    FileOutputStream out = atomic.startWrite();
    try {
      Os.fchmod(out.getFD(), 0600);
      out.write(json.getBytes(StandardCharsets.UTF_8));
      atomic.finishWrite(out);
    } catch (Exception error) {
      atomic.failWrite(out);
      throw error;
    }
    return path;
  }
}
