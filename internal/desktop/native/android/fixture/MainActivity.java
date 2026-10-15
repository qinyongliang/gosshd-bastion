package com.example.gosshd.fixture;

import android.app.Activity;
import android.os.Bundle;
import android.text.InputType;
import android.view.View;
import android.view.WindowManager;
import android.widget.*;

// Owned test UI only; no network, filesystem or private data access.
public final class MainActivity extends Activity {
  private int count;
  private int dp(int value) { return (int)(value * getResources().getDisplayMetrics().density); }
  @Override public void onCreate(Bundle saved) {
    super.onCreate(saved);
    getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
    getWindow().setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_STATE_ALWAYS_HIDDEN);
    LinearLayout root = new LinearLayout(this);
    root.setOrientation(LinearLayout.VERTICAL); root.setPadding(dp(16), dp(12), dp(16), dp(12));
    TextView title = new TextView(this); title.setText("GOSSHD Android desktop fixture"); title.setTextSize(20); root.addView(title);
    final TextView result = new TextView(this); result.setId(R.id.result); result.setText("Ready"); result.setTextSize(18); root.addView(result);
    final EditText editor = new EditText(this); editor.setId(R.id.editor); editor.setHint("Type literal text"); editor.setSingleLine(true);
    editor.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS); editor.setShowSoftInputOnFocus(false); root.addView(editor);
    Button tap = new Button(this); tap.setId(R.id.tap); tap.setText("Tap fixture"); root.addView(tap);
    tap.setOnClickListener(v -> result.setText("Tapped " + (++count)));
    tap.setOnLongClickListener(v -> { result.setText("Long pressed"); return true; });
    CheckBox toggle = new CheckBox(this); toggle.setId(R.id.toggle); toggle.setText("Toggle fixture"); root.addView(toggle);
    toggle.setOnCheckedChangeListener((button, checked) -> result.setText(checked ? "Checked" : "Unchecked"));
    SeekBar slider = new SeekBar(this); slider.setId(R.id.slider); slider.setMax(100); root.addView(slider);
    slider.setOnSeekBarChangeListener(new SeekBar.OnSeekBarChangeListener() {
      public void onProgressChanged(SeekBar bar, int value, boolean user) { if(user) result.setText("Slider " + value); }
      public void onStartTrackingTouch(SeekBar bar) {} public void onStopTrackingTouch(SeekBar bar) {}
    });
    ScrollView scroll = new ScrollView(this); scroll.setId(R.id.scroll); root.addView(scroll,new LinearLayout.LayoutParams(-1,0,1));
    LinearLayout items = new LinearLayout(this); items.setOrientation(LinearLayout.VERTICAL); scroll.addView(items);
    for(int i=0;i<35;i++) {TextView item=new TextView(this);item.setText("Fixture row " + i);item.setTextSize(18);item.setPadding(0,dp(10),0,dp(10));items.addView(item);}
    setContentView(root); root.setFocusableInTouchMode(true); root.requestFocus();
  }
  // Passive second service used only to verify activation preserves other grants.
  public static final class PassiveAccessibilityService extends android.accessibilityservice.AccessibilityService {
    public void onAccessibilityEvent(android.view.accessibility.AccessibilityEvent event) {}
    public void onInterrupt() {}
  }
}
