package com.gosshd.desktop.access;

import android.accessibilityservice.AccessibilityService;
import android.os.Bundle;
import android.view.accessibility.AccessibilityNodeInfo;
import android.view.accessibility.AccessibilityWindowInfo;
import org.json.JSONObject;

// Main-thread semantic input; no shell, IME switching or privileged APIs.
public final class NodeActions {
  public static void execute(
      String method,
      JSONObject p,
      DesktopAccessibilityService service,
      AccessibilityWindowInfo window,
      AccessibilityNodeInfo node)
      throws Exception {
    switch (method) {
      case "activate_window":
        return; // Backend already requires active/focused identity.
      case "click":
        if (!p.optString("button", "left").equals("left"))
          throw new IllegalStateException("Only left click supported");
        int count = p.optInt("click_count", 1);
        if (count == 0) count = 1;
        if (count < 1 || count > 2) throw new IllegalArgumentException("Invalid click count");
        for (int i = 0; i < count; i++) {
          if (!node.refresh()) throw new IllegalStateException("Element disappeared");
          perform(node, AccessibilityNodeInfo.ACTION_CLICK, null);
        }
        return;
      case "set_value":
        setText(node, p.optString("text"));
        return;
      case "type_text":
      case "press_key":
        if (method.equals("press_key")) {
          String key = p.optString("key").toLowerCase(java.util.Locale.ROOT);
          int global =
              key.equals("back")
                  ? AccessibilityService.GLOBAL_ACTION_BACK
                  : key.equals("home")
                      ? AccessibilityService.GLOBAL_ACTION_HOME
                      : key.equals("recents") ? AccessibilityService.GLOBAL_ACTION_RECENTS : 0;
          if (global != 0) {
            if (!service.performGlobalAction(global))
              throw new IllegalStateException("Global action rejected");
            return;
          }
          if (!key.equals("ctrl+a"))
            throw new IllegalStateException("Auxiliary keys: Back/Home/Recents or editable Ctrl+a");
        }
        AccessibilityNodeInfo root = window.getRoot();
        if (root == null) throw new IllegalStateException("Window has no root");
        AccessibilityNodeInfo focused = root.findFocus(AccessibilityNodeInfo.FOCUS_INPUT);
        root.recycle();
        if (focused == null) throw new IllegalStateException("Focus an editable field first");
        try {
          if (!focused.refresh() || focused.isPassword() || !focused.isEditable())
            throw new IllegalStateException("Editable non-secure focus required");
          String old =
              focused.isShowingHintText() ? "" : AccessibilityTree.text(focused.getText(), 16384);
          if (method.equals("press_key")) {
            Bundle selection = new Bundle();
            selection.putInt(AccessibilityNodeInfo.ACTION_ARGUMENT_SELECTION_START_INT, 0);
            selection.putInt(AccessibilityNodeInfo.ACTION_ARGUMENT_SELECTION_END_INT, old.length());
            perform(focused, AccessibilityNodeInfo.ACTION_SET_SELECTION, selection);
          } else {
            int start = focused.getTextSelectionStart(), end = focused.getTextSelectionEnd();
            if (start < 0 || end < 0) {
              start = old.length();
              end = start;
            }
            if (start > old.length() || end > old.length())
              throw new IllegalStateException("Text selection changed; observe again");
            setText(
                focused,
                old.substring(0, Math.min(start, end))
                    + p.optString("text")
                    + old.substring(Math.max(start, end)));
          }
        } finally {
          focused.recycle();
        }
        return;
      case "scroll":
        double dy = p.optDouble("delta_y", 0), dx = p.optDouble("delta_x", 0);
        if (dy == 0 && dx == 0) throw new IllegalArgumentException("Nonzero scroll delta required");
        int action =
            dy != 0
                ? (dy > 0
                    ? AccessibilityNodeInfo.ACTION_SCROLL_FORWARD
                    : AccessibilityNodeInfo.ACTION_SCROLL_BACKWARD)
                : (dx > 0
                    ? AccessibilityNodeInfo.AccessibilityAction.ACTION_SCROLL_RIGHT.getId()
                    : AccessibilityNodeInfo.AccessibilityAction.ACTION_SCROLL_LEFT.getId());
        perform(node, action, null);
        return;
      case "perform_action":
        String requested = p.optString("action");
        if (requested.equals("toggle") && !node.isCheckable())
          throw new IllegalStateException("Element is not checkable");
        int code =
            requested.equals("invoke") || requested.equals("toggle")
                ? AccessibilityNodeInfo.ACTION_CLICK
                : requested.equals("secondary")
                    ? AccessibilityNodeInfo.ACTION_LONG_CLICK
                    : requested.equals("focus")
                        ? AccessibilityNodeInfo.ACTION_FOCUS
                        : requested.equals("select") ? AccessibilityNodeInfo.ACTION_SELECT : 0;
        if (code == 0) throw new IllegalStateException("Unsupported native action");
        perform(node, code, null);
        return;
      default:
        throw new IllegalArgumentException("Unsupported desktop method");
    }
  }

  private static void perform(AccessibilityNodeInfo node, int action, Bundle args) {
    if (node == null || !AccessibilityTree.has(node, action) || !node.performAction(action, args))
      throw new IllegalStateException("Native accessibility action rejected");
  }

  private static void setText(AccessibilityNodeInfo node, String value) {
    if (node == null || !node.isEditable() || node.isPassword() || value.length() > 16384)
      throw new IllegalStateException("Editable non-secure field and bounded text required");
    Bundle args = new Bundle();
    args.putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, value);
    perform(node, AccessibilityNodeInfo.ACTION_SET_TEXT, args);
  }
}
