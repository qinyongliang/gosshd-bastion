package com.gosshd.desktop.access;

import android.graphics.Rect;
import android.view.accessibility.AccessibilityNodeInfo;
import java.util.HashSet;
import org.json.JSONArray;
import org.json.JSONObject;

public final class AccessibilityTree {
  private final Observation observation;
  private final int maxDepth, maxElements;
  private final HashSet<Integer> seen = new HashSet<>();
  private final JSONArray tree = new JSONArray();
  private boolean truncated;

  public AccessibilityTree(Observation o, int depth, int limit) {
    observation = o;
    maxDepth = depth;
    maxElements = limit;
  }

  public JSONArray build(AccessibilityNodeInfo root) throws Exception {
    walk(root, -1, 0);
    return tree;
  }

  public boolean truncated() {
    return truncated;
  }

  private void walk(AccessibilityNodeInfo node, int parent, int depth) throws Exception {
    if (node == null) return;
    // Refresh each node: framework caching can outlive a preceding ACTION_SET_TEXT.
    if (!node.refresh()) {
      node.recycle();
      return;
    }
    if (node.isPassword()
        || !node.isVisibleToUser()
        || !observation.app.equals(String.valueOf(node.getPackageName()))) {
      node.recycle();
      return;
    }
    if (tree.length() >= maxElements) {
      truncated = true;
      node.recycle();
      return;
    }
    int hash = node.hashCode();
    if (!seen.add(hash)) {
      node.recycle();
      return;
    }
    Rect bounds = new Rect();
    node.getBoundsInScreen(bounds);
    if (!bounds.intersect(observation.bounds)) {
      node.recycle();
      return;
    }
    int index = tree.length();
    observation.nodes.add(node);
    JSONArray actions = new JSONArray();
    if (node.isEnabled()) {
      if (has(node, AccessibilityNodeInfo.ACTION_CLICK)) actions.put("invoke");
      if (has(node, AccessibilityNodeInfo.ACTION_LONG_CLICK)) actions.put("secondary");
      if (node.isCheckable() && has(node, AccessibilityNodeInfo.ACTION_CLICK))
        actions.put("toggle");
      if (has(node, AccessibilityNodeInfo.ACTION_SET_TEXT)) actions.put("set_value");
      if (has(node, AccessibilityNodeInfo.ACTION_FOCUS)) actions.put("focus");
      if (has(node, AccessibilityNodeInfo.ACTION_SELECT)) actions.put("select");
      if (node.isScrollable()) actions.put("scroll");
    }
    String value = text(node.getText(), 4096), name = text(node.getContentDescription(), 4096);
    if (name.isEmpty()) name = value;
    tree.put(
        new JSONObject()
            .put("index", index)
            .put("parent", parent)
            .put("role", String.valueOf(node.getClassName()))
            .put("name", name)
            .put("value", value)
            .put("automation_id", text(node.getViewIdResourceName(), 512))
            .put("enabled", node.isEnabled())
            .put("focused", node.isFocused())
            .put("selected", node.isSelected())
            .put("toggle_state", node.isCheckable() ? (node.isChecked() ? "on" : "off") : "")
            .put(
                "bounds",
                rect(
                    new Rect(
                        bounds.left - observation.bounds.left,
                        bounds.top - observation.bounds.top,
                        bounds.right - observation.bounds.left,
                        bounds.bottom - observation.bounds.top)))
            .put("actions", actions));
    if (depth >= maxDepth) {
      if (node.getChildCount() > 0) truncated = true;
      return;
    }
    for (int i = 0; i < node.getChildCount(); i++) {
      if (tree.length() >= maxElements) {
        truncated = true;
        break;
      }
      walk(node.getChild(i), index, depth + 1);
    }
  }

  public static boolean has(AccessibilityNodeInfo node, int action) {
    for (AccessibilityNodeInfo.AccessibilityAction a : node.getActionList())
      if (a.getId() == action) return true;
    return false;
  }

  public static JSONObject rect(Rect r) throws Exception {
    return new JSONObject()
        .put("x", r.left)
        .put("y", r.top)
        .put("width", r.width())
        .put("height", r.height());
  }

  public static String text(CharSequence value, int max) {
    String t = value == null ? "" : value.toString();
    return t.length() > max ? t.substring(0, max) : t;
  }
}
