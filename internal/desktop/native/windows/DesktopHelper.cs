// Independent Windows JSON-lines helper. No GOSSHD/network dependencies.
using System;
using System.Collections;
using System.Collections.Generic;
using System.Diagnostics;
using System.Drawing;
using System.Drawing.Imaging;
using System.Globalization;
using System.IO;
using System.IO.Pipes;
using System.Security.Principal;
using System.Linq;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;
using System.Web.Script.Serialization;
using System.Windows.Automation;

public static class DesktopHelper {
 static JavaScriptSerializer json = new JavaScriptSerializer { MaxJsonLength = 16*1024*1024, RecursionLimit=64 };
 static string snapshot=""; static IntPtr observed; static string observedApp=""; static DateTime observedAt;
 static RECT observedRect; static List<AutomationElement> refs=new List<AutomationElement>();
 delegate bool EnumProc(IntPtr h,IntPtr p);
 delegate bool MonitorProc(IntPtr monitor,IntPtr dc,ref RECT rect,IntPtr data);
 [StructLayout(LayoutKind.Sequential)] struct MONITORINFO {public uint cbSize;public RECT monitor,work;public uint flags;}
 [DllImport("user32.dll")] static extern bool EnumDisplayMonitors(IntPtr dc,IntPtr clip,MonitorProc callback,IntPtr data);
 [DllImport("user32.dll")] static extern bool GetMonitorInfo(IntPtr monitor,ref MONITORINFO info);
 [StructLayout(LayoutKind.Sequential)] struct RECT { public int Left,Top,Right,Bottom; }
 [StructLayout(LayoutKind.Sequential)] struct POINT { public int X,Y; }
 [StructLayout(LayoutKind.Sequential)] struct INPUT { public uint type; public InputUnion u; }
 [StructLayout(LayoutKind.Explicit)] struct InputUnion { [FieldOffset(0)] public MOUSEINPUT mouse; [FieldOffset(0)] public KEYBDINPUT key; }
 [StructLayout(LayoutKind.Sequential)] struct MOUSEINPUT { public int dx,dy; public uint mouseData,dwFlags,time; public UIntPtr extra; }
 [StructLayout(LayoutKind.Sequential)] struct KEYBDINPUT { public ushort vk,scan;public uint flags,time;public UIntPtr extra; }
 [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc proc,IntPtr p);
 [DllImport("user32.dll")] static extern bool IsWindow(IntPtr h);
 [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowText(IntPtr h,StringBuilder text,int max);
 [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h,out uint pid);
 [DllImport("user32.dll")] static extern bool GetWindowRect(IntPtr h,out RECT rect);
 [DllImport("user32.dll")] static extern bool SetForegroundWindow(IntPtr h);
 [DllImport("user32.dll")] static extern IntPtr GetForegroundWindow();
 [DllImport("user32.dll")] static extern bool ShowWindow(IntPtr h,int cmd);
 [DllImport("user32.dll")] static extern bool PrintWindow(IntPtr h,IntPtr dc,uint flags);
 [DllImport("user32.dll")] static extern bool SetCursorPos(int x,int y);
 [DllImport("user32.dll")] static extern bool GetCursorPos(out POINT point);
 [DllImport("user32.dll")] static extern IntPtr WindowFromPoint(POINT point);
 [DllImport("user32.dll",SetLastError=true)] static extern IntPtr SendMessageTimeout(IntPtr h,uint message,UIntPtr wParam,IntPtr lParam,uint flags,uint timeout,out UIntPtr result);
 [DllImport("user32.dll")] static extern IntPtr GetAncestor(IntPtr window,uint flags);
 [DllImport("user32.dll",SetLastError=true)] static extern uint SendInput(uint n,INPUT[] inputs,int size);
 [DllImport("user32.dll")] static extern bool SetProcessDpiAwarenessContext(IntPtr context);
 [DllImport("user32.dll")] static extern bool SetProcessDPIAware();
 [DllImport("user32.dll")] static extern IntPtr GetWindowDpiAwarenessContext(IntPtr window);
 [DllImport("user32.dll")] static extern IntPtr SetThreadDpiAwarenessContext(IntPtr context);
 [DllImport("kernel32.dll")] static extern bool ProcessIdToSessionId(uint pid,out uint session);
 static Dictionary<string,object> Map(object o){return o as Dictionary<string,object> ?? new Dictionary<string,object>();}
 static string S(Dictionary<string,object> p,string k,string d=""){return p.ContainsKey(k)?Convert.ToString(p[k]):d;}
 static double N(Dictionary<string,object> p,string k,double d=0){return p.ContainsKey(k)?Convert.ToDouble(p[k]):d;}
 static bool B(Dictionary<string,object> p,string k,bool d=false){return p.ContainsKey(k)?Convert.ToBoolean(p[k]):d;}
 // The native boundary also enforces mode: callers may bypass the Go validator.
 sealed class BackgroundError:Exception {public BackgroundError(string reason):base("background_not_supported: "+reason){}}
 static string ExecutionMode(Dictionary<string,object> p){string mode=S(p,"mode");return mode==""?"default":mode;}
 static bool Background(Dictionary<string,object> p){return S(p,"mode")=="background";}
 static Exception Unsupported(bool background,string reason){return background?(Exception)new BackgroundError(reason):new Exception(reason);}
 static void ValidateMode(string method,Dictionary<string,object> p){
  if(p.ContainsKey("mode")&&p["mode"]!=null&&!(p["mode"] is string))throw new Exception("mode must be default, auto or background");
  string mode=S(p,"mode");if(mode!=""&&mode!="default"&&mode!="auto"&&mode!="background")throw new Exception("mode must be default, auto or background");
  if(!Background(p))return;
  switch(method){
   case "activate_window":case "launch_app":case "type_text":case "press_key":case "drag":case "request_permissions":throw new BackgroundError(method+" requires foreground input or user interaction");
   case "perform_action":if(S(p,"action")=="focus")throw new BackgroundError("focus changes the shared desktop focus");break;
   case "click":if(!p.ContainsKey("element_index")||p["element_index"]==null||(S(p,"button")!=""&&S(p,"button")!="left")||N(p,"click_count",1)>1)throw new BackgroundError("click requires an element supporting a single semantic invoke");break;
   case "scroll":if(!p.ContainsKey("element_index")||p["element_index"]==null)throw new BackgroundError("scroll requires native semantic scrolling");break;
   case "screenshot":var w=Map(p.ContainsKey("window")?p["window"]:null);if(S(w,"id")==""||S(w,"app")=="")throw new BackgroundError("capture requires a specific window");break;
  }
 }
 // Fail closed before visible-screen fallback. Independently testable without a GUI.
 static void RequireScreenFallback(bool background){if(background)throw new BackgroundError("PrintWindow failed; visible-screen fallback is forbidden");}
 static object Window(IntPtr h){uint pid;GetWindowThreadProcessId(h,out pid);var t=new StringBuilder(513);GetWindowText(h,t,513);return new {id=h.ToInt64().ToString(),app=pid.ToString(),title=t.ToString()};}
 static IntPtr Target(Dictionary<string,object> p){var w=Map(p.ContainsKey("window")?p["window"]:null);long id;if(!long.TryParse(S(w,"id"),out id))throw new Exception("invalid window id");var h=new IntPtr(id);uint pid;GetWindowThreadProcessId(h,out pid);if(!IsWindow(h)||pid.ToString()!=S(w,"app"))throw new Exception("window no longer exists; enumerate again");return h;}
 static object Bounds(RECT r){return new {x=(double)r.Left,y=(double)r.Top,width=(double)(r.Right-r.Left),height=(double)(r.Bottom-r.Top)};}
 static RECT Rect(IntPtr h){RECT r;if(!GetWindowRect(h,out r)||r.Right<=r.Left||r.Bottom<=r.Top)throw new Exception("window has no visible bounds");return r;}
 static object ListWindows(){var list=new List<object>();EnumWindows(delegate(IntPtr h,IntPtr p){if(IsWindowVisible(h)){var t=new StringBuilder(513);GetWindowText(h,t,513);if(t.Length>0)list.Add(Window(h));}return list.Count<512;},IntPtr.Zero);return list;}
 static List<object> Displays(){var displays=new List<object>();EnumDisplayMonitors(IntPtr.Zero,IntPtr.Zero,delegate(IntPtr h,IntPtr dc,ref RECT rect,IntPtr data){var info=new MONITORINFO{cbSize=(uint)Marshal.SizeOf(typeof(MONITORINFO))};if(GetMonitorInfo(h,ref info))displays.Add(new {id=h.ToInt64().ToString(),bounds=Bounds(info.monitor),primary=(info.flags&1)!=0});return displays.Count<32;},IntPtr.Zero);return displays;}
 static RECT DisplayRect(string id){RECT result=new RECT();bool found=false;EnumDisplayMonitors(IntPtr.Zero,IntPtr.Zero,delegate(IntPtr h,IntPtr dc,ref RECT rect,IntPtr data){var info=new MONITORINFO{cbSize=(uint)Marshal.SizeOf(typeof(MONITORINFO))};if(GetMonitorInfo(h,ref info)&&((id==""&&(info.flags&1)!=0)||id==h.ToInt64().ToString())){result=info.monitor;found=true;return false;}return true;},IntPtr.Zero);if(!found)throw new Exception("display not found; enumerate again");return result;}
 static object ListApps(){var groups=new Dictionary<string,List<object>>();foreach(var w in (List<object>)ListWindows()){var m=Map(json.DeserializeObject(json.Serialize(w)));string app=S(m,"app");if(!groups.ContainsKey(app))groups[app]=new List<object>();groups[app].Add(w);}return groups.Select(x=>new {id=x.Key,windows=x.Value}).ToArray();}
 static void Available(){uint session;ProcessIdToSessionId((uint)Process.GetCurrentProcess().Id,out session);if(session==0)throw new Exception("no interactive desktop: run a desktop-enabled Agent in a logged-in user session, not Session 0");}
 static object Status(){uint session;ProcessIdToSessionId((uint)Process.GetCurrentProcess().Id,out session);return new {platform="windows",interactive=session!=0,permissions=new {screen_capture=session!=0,accessibility=session!=0},capture_backend="PrintWindow; visible-screen fallback",protocol_version=2,background_supported=true,supported_modes=new[]{"default","auto","background"},background_capture_backend="PrintWindow (no screen fallback)"};}
 static void Clear(){snapshot="";refs.Clear();observed=IntPtr.Zero;}
 static IntPtr ValidateSnapshot(Dictionary<string,object> p){var h=Target(p);uint pid;GetWindowThreadProcessId(h,out pid);if(snapshot==""||snapshot!=S(p,"snapshot_id")||h!=observed||observedApp!=pid.ToString()||(DateTime.UtcNow-observedAt).TotalSeconds>60)throw new Exception("stale snapshot; observe this window again");var r=Rect(h);if(r.Right-r.Left!=observedRect.Right-observedRect.Left||r.Bottom-r.Top!=observedRect.Bottom-observedRect.Top)throw new Exception("window resized; observe again");return h;}
 static void Activate(IntPtr h){if(GetForegroundWindow()==h)return;ShowWindow(h,9);SetForegroundWindow(h);Thread.Sleep(100);if(GetForegroundWindow()!=h){try{AutomationElement.FromHandle(h).SetFocus();}catch{}Thread.Sleep(100);}if(GetForegroundWindow()!=h)throw new Exception("cannot activate target window; click a visible target element first or activate it locally");}
 static AutomationElement Element(Dictionary<string,object> p){int i=(int)N(p,"element_index",-1);if(i<0||i>=refs.Count)throw new Exception("element_index is not in the latest observation");return refs[i];}
 static string Limit(string value,int max){return value==null?"":value.Length>max?value.Substring(0,max):value;}
 static List<string> Actions(AutomationElement e){var result=new List<string>();object pattern;if(e.TryGetCurrentPattern(InvokePattern.Pattern,out pattern))result.Add("invoke");if(e.TryGetCurrentPattern(ValuePattern.Pattern,out pattern)&&!((ValuePattern)pattern).Current.IsReadOnly)result.Add("set_value");if(e.TryGetCurrentPattern(RangeValuePattern.Pattern,out pattern)&&!((RangeValuePattern)pattern).Current.IsReadOnly)result.Add("set_value");if(e.TryGetCurrentPattern(TogglePattern.Pattern,out pattern))result.Add("toggle");if(e.TryGetCurrentPattern(ExpandCollapsePattern.Pattern,out pattern)){result.Add("expand");result.Add("collapse");}if(e.TryGetCurrentPattern(SelectionItemPattern.Pattern,out pattern))result.Add("select");if(e.TryGetCurrentPattern(ScrollPattern.Pattern,out pattern))result.Add("scroll");if(e.Current.IsKeyboardFocusable)result.Add("focus");return result;}
 static void Walk(AutomationElement e,int parent,int depth,int maxDepth,int max,List<object> tree,RECT origin,ref bool truncated){
  if(e==null)return;if(tree.Count>=max){truncated=true;return;}
  try{var c=e.Current;if(c.IsOffscreen||c.IsPassword)return;var b=c.BoundingRectangle;int index=refs.Count;refs.Add(e);string value="";object vp;if(e.TryGetCurrentPattern(ValuePattern.Pattern,out vp)){value=((ValuePattern)vp).Current.Value;if(value.Length>4096)value=value.Substring(0,4096);}
   if(value==""&&e.TryGetCurrentPattern(TextPattern.Pattern,out vp))value=((TextPattern)vp).DocumentRange.GetText(4096);
   object selected=null,expanded=null;string toggle="";double? minimum=null,maximum=null,verticalPercent=null,horizontalPercent=null;object verticalScrollable=null,horizontalScrollable=null;
   if(e.TryGetCurrentPattern(SelectionItemPattern.Pattern,out vp))selected=((SelectionItemPattern)vp).Current.IsSelected;
   if(e.TryGetCurrentPattern(ExpandCollapsePattern.Pattern,out vp))expanded=((ExpandCollapsePattern)vp).Current.ExpandCollapseState==ExpandCollapseState.Expanded;
   if(e.TryGetCurrentPattern(TogglePattern.Pattern,out vp))toggle=((TogglePattern)vp).Current.ToggleState.ToString().ToLowerInvariant();
   if(e.TryGetCurrentPattern(RangeValuePattern.Pattern,out vp)){var range=((RangeValuePattern)vp).Current;value=range.Value.ToString(CultureInfo.InvariantCulture);minimum=range.Minimum;maximum=range.Maximum;}
   if(e.TryGetCurrentPattern(ScrollPattern.Pattern,out vp)){var scrolling=((ScrollPattern)vp).Current;verticalScrollable=scrolling.VerticallyScrollable;horizontalScrollable=scrolling.HorizontallyScrollable;verticalPercent=scrolling.VerticalScrollPercent;horizontalPercent=scrolling.HorizontalScrollPercent;}
   tree.Add(new {index=index,parent=parent,role=c.ControlType.ProgrammaticName,name=Limit(c.Name,4096),value=Limit(value,4096),automation_id=Limit(c.AutomationId,512),help=Limit(c.HelpText,1024),focused=c.HasKeyboardFocus,selected=selected,expanded=expanded,toggle_state=toggle,minimum=minimum,maximum=maximum,vertical_scrollable=verticalScrollable,horizontal_scrollable=horizontalScrollable,vertical_scroll_percent=verticalPercent,horizontal_scroll_percent=horizontalPercent,enabled=c.IsEnabled,bounds=new {x=b.X-origin.Left,y=b.Y-origin.Top,width=b.Width,height=b.Height},actions=Actions(e)});
   if(depth>=maxDepth){if(TreeWalker.ControlViewWalker.GetFirstChild(e)!=null)truncated=true;return;}
   var child=TreeWalker.ControlViewWalker.GetFirstChild(e);while(child!=null&&tree.Count<max){Walk(child,index,depth+1,maxDepth,max,tree,origin,ref truncated);child=TreeWalker.ControlViewWalker.GetNextSibling(child);}if(child!=null)truncated=true;
  }catch(ElementNotAvailableException){}catch(InvalidOperationException){}
 }
 static void CaptureLimits(int width,int height){if(width<=0||height<=0||width>16384||height>16384||(long)width*height>64*1024*1024)throw new Exception("window exceeds capture limits");}
 static Bitmap WindowBitmap(IntPtr h,RECT physical){
  if(h==IntPtr.Zero)return null;
  IntPtr previous=IntPtr.Zero;Bitmap bitmap=null;
  try{
   // Legacy apps print in their own logical units. Preserve that image size;
   // the response bounds and input coordinates remain native physical units.
   try{var context=GetWindowDpiAwarenessContext(h);if(context!=IntPtr.Zero)previous=SetThreadDpiAwarenessContext(context);}catch(EntryPointNotFoundException){}
   RECT logical=previous==IntPtr.Zero?physical:Rect(h);
   int width=logical.Right-logical.Left,height=logical.Bottom-logical.Top;CaptureLimits(width,height);
   bitmap=new Bitmap(width,height,PixelFormat.Format32bppArgb);bool captured;
   using(var g=Graphics.FromImage(bitmap)){IntPtr dc=g.GetHdc();try{captured=PrintWindow(h,dc,2);}finally{g.ReleaseHdc(dc);}}
   if(captured)return bitmap;
   bitmap.Dispose();return null;
  }catch{if(bitmap!=null)bitmap.Dispose();throw;}
  finally{if(previous!=IntPtr.Zero)SetThreadDpiAwarenessContext(previous);}
 }
 static Bitmap ScreenBitmap(RECT r){int width=r.Right-r.Left,height=r.Bottom-r.Top;var bitmap=new Bitmap(width,height,PixelFormat.Format32bppArgb);try{using(var g=Graphics.FromImage(bitmap))g.CopyFromScreen(r.Left,r.Top,0,0,new Size(width,height),CopyPixelOperation.SourceCopy);return bitmap;}catch{bitmap.Dispose();throw;}}
 static Bitmap ChooseBitmap(bool background,Func<Bitmap> windowCapture,Func<Bitmap> screenCapture,out string method){
  method="print_window";var bitmap=windowCapture();if(bitmap!=null)return bitmap;
  RequireScreenFallback(background);method="visible_screen";return screenCapture();
 }
 static object Screenshot(IntPtr h,RECT r,bool background){
  if(background&&h==IntPtr.Zero)throw new BackgroundError("capture requires a specific window");
  int width=r.Right-r.Left,height=r.Bottom-r.Top;CaptureLimits(width,height);string method;
  using(var bitmap=ChooseBitmap(background,()=>WindowBitmap(h,r),()=>ScreenBitmap(r),out method)){
   double scale=Math.Min(1.0,1600.0/Math.Max(bitmap.Width,bitmap.Height));int w=Math.Max(1,(int)(bitmap.Width*scale)),hh=Math.Max(1,(int)(bitmap.Height*scale));
   using(var resized=new Bitmap(bitmap,w,hh))using(var ms=new MemoryStream()){resized.Save(ms,ImageFormat.Png);if(ms.Length>8*1024*1024)throw new Exception("screenshot exceeds size limit");return new {capture_method=method,mime_type="image/png",data=Convert.ToBase64String(ms.ToArray()),width=w,height=hh};}
  }

 }
 static object CaptureScreenshot(Dictionary<string,object> p){Available();Clear();bool window=p.ContainsKey("window");var h=window?Target(p):IntPtr.Zero;var r=window?Rect(h):DisplayRect(S(p,"display_id"));return new {snapshot_id=Guid.NewGuid().ToString(),window=window?Window(h):new {id="",app=""},bounds=Bounds(r),mode=ExecutionMode(p),image=Screenshot(h,r,Background(p))};}
 static object Observe(Dictionary<string,object> p){Available();var h=Target(p);Clear();RECT r=Rect(h);var tree=new List<object>();bool truncated=false;if(B(p,"include_text")){int depth=(int)N(p,"max_depth",8),max=(int)N(p,"max_elements",512);if(depth<1||depth>16||max<1||max>1024)throw new Exception("invalid accessibility limits");Walk(AutomationElement.FromHandle(h),-1,0,depth,max,tree,r,ref truncated);}
  object image=B(p,"include_screenshot",true)?Screenshot(h,r,Background(p)):null;snapshot=Guid.NewGuid().ToString();observed=h;observedApp=S(Map(p["window"]),"app");observedRect=r;observedAt=DateTime.UtcNow;return new {snapshot_id=snapshot,window=Window(h),bounds=Bounds(r),observation_method=B(p,"include_text")?"uia.control_view":"window_bounds",mode=ExecutionMode(p),elements=tree,image=image,truncated=truncated};}
 static void Send(params INPUT[] input){if(SendInput((uint)input.Length,input,Marshal.SizeOf(typeof(INPUT)))!=input.Length)throw new Exception("input injection rejected (possibly a higher-integrity or secure desktop)");}
 static INPUT Mouse(uint flag,uint data=0){return new INPUT{type=0,u=new InputUnion{mouse=new MOUSEINPUT{dwFlags=flag,mouseData=data}}};}
 static INPUT Key(ushort vk,uint flags=0,ushort scan=0){return new INPUT{type=1,u=new InputUnion{key=new KEYBDINPUT{vk=vk,flags=flags,scan=scan}}};}
 static void Position(IntPtr h,double x,double y){RECT r=Rect(h);if(x<0||y<0||x>=r.Right-r.Left||y>=r.Bottom-r.Top)throw new Exception("coordinate is outside the observed window");var point=new POINT{X=r.Left+(int)x,Y=r.Top+(int)y};if(GetAncestor(WindowFromPoint(point),2)!=h)throw new Exception("target point is occluded; no input was sent");if(!SetCursorPos(point.X,point.Y))throw new Exception("cannot position cursor");}
 static void Wheel(IntPtr h,int delta,uint message){if(delta==0)return;POINT point;if(!GetCursorPos(out point)||GetAncestor(WindowFromPoint(point),2)!=h)throw new Exception("scroll target changed; observe again");if(point.X<short.MinValue||point.X>short.MaxValue||point.Y<short.MinValue||point.Y>short.MaxValue)throw new Exception("wheel coordinates exceed native message bounds");var target=WindowFromPoint(point);uint coordinates=(uint)(ushort)point.X|((uint)(ushort)point.Y<<16);UIntPtr result;if(SendMessageTimeout(target,message,new UIntPtr(unchecked((uint)(delta<<16))),new IntPtr(unchecked((int)coordinates)),2,1000,out result)==IntPtr.Zero)throw new Exception("targeted wheel input was rejected or timed out");}
 static ushort KeyCode(string key){string k=key.ToLowerInvariant();switch(k){case "ctrl":case "control":case "control_l":return 0x11;case "alt":case "alt_l":return 0x12;case "shift":case "shift_l":return 0x10;case "meta":case "command":case "super_l":return 0x5B;case "return":case "enter":return 0x0D;case "tab":return 9;case "escape":case "esc":return 0x1B;case "space":return 0x20;case "backspace":return 8;case "delete":return 0x2E;case "left":return 0x25;case "up":return 0x26;case "right":return 0x27;case "down":return 0x28;case "home":return 0x24;case "end":return 0x23;case "pageup":case "page_up":return 0x21;case "pagedown":case "page_down":return 0x22;case "period":case ".":return 0xBE;case "comma":case ",":return 0xBC;case "minus":case "-":return 0xBD;}
  int f;if(k.StartsWith("f")&&int.TryParse(k.Substring(1),out f)&&f>=1&&f<=12)return (ushort)(0x6F+f);if(k.Length==1&&char.IsLetterOrDigit(k[0]))return (ushort)char.ToUpperInvariant(k[0]);throw new Exception("unsupported key name");}
 static void Chord(string chord){var keys=chord.Split('+').Select(x=>KeyCode(x.Trim())).ToArray();if(keys.Length<1||keys.Length>5)throw new Exception("invalid key chord");int pressed=0;try{foreach(var key in keys){Send(Key(key));pressed++;}}finally{for(int i=pressed-1;i>=0;i--)Send(Key(keys[i],2));}}
 static ScrollAmount ScrollStep(double delta){if(delta==0)return ScrollAmount.NoAmount;return delta>0?(delta>120?ScrollAmount.LargeIncrement:ScrollAmount.SmallIncrement):(delta< -120?ScrollAmount.LargeDecrement:ScrollAmount.SmallDecrement);}
 static string Action(AutomationElement e,string action,bool background){
  object pattern;
  switch(action){
   case "focus":if(background)throw new BackgroundError("focus is forbidden");e.SetFocus();return "uia.focus";
   case "invoke":if(e.TryGetCurrentPattern(InvokePattern.Pattern,out pattern)){((InvokePattern)pattern).Invoke();return "uia.invoke";}break;
   case "toggle":if(e.TryGetCurrentPattern(TogglePattern.Pattern,out pattern)){((TogglePattern)pattern).Toggle();return "uia.toggle";}break;
   case "expand":case "collapse":if(e.TryGetCurrentPattern(ExpandCollapsePattern.Pattern,out pattern)){if(action=="expand")((ExpandCollapsePattern)pattern).Expand();else ((ExpandCollapsePattern)pattern).Collapse();return "uia."+action;}break;
   case "select":if(e.TryGetCurrentPattern(SelectionItemPattern.Pattern,out pattern)){((SelectionItemPattern)pattern).Select();return "uia.selection_item";}break;
  }
  throw Unsupported(background,"accessibility action is not supported by this element");
 }
 static object Operate(string method,Dictionary<string,object> p){
  Available();bool background=Background(p);string used="";
  if(method=="launch_app"){string app=S(p,"app");if(!Path.IsPathRooted(app)||!Path.GetFullPath(app).Equals(app,StringComparison.OrdinalIgnoreCase)||!app.EndsWith(".exe",StringComparison.OrdinalIgnoreCase)||!File.Exists(app))throw new Exception("launch_app requires an absolute existing .exe path");Clear();Process.Start(new ProcessStartInfo(app){UseShellExecute=true});return new {ok=true,operation_method="launch_app",mode=ExecutionMode(p)};}
  var h=ValidateSnapshot(p);// References stay private until this operation finishes.
  try{
   switch(method){
    case "activate_window":Activate(h);used="foreground_activation";break;
    case "set_value":{
     var e=Element(p);object pattern;if(e.Current.IsPassword)throw new Exception("password elements are not exposed");
     if(e.TryGetCurrentPattern(ValuePattern.Pattern,out pattern)&&!((ValuePattern)pattern).Current.IsReadOnly){((ValuePattern)pattern).SetValue(S(p,"text"));used="uia.value";}
     else if(e.TryGetCurrentPattern(RangeValuePattern.Pattern,out pattern)&&!((RangeValuePattern)pattern).Current.IsReadOnly){double v;if(!double.TryParse(S(p,"text"),NumberStyles.Float,CultureInfo.InvariantCulture,out v)||double.IsNaN(v)||double.IsInfinity(v))throw new Exception("range value must be finite numeric text");var range=((RangeValuePattern)pattern).Current;if(v<range.Minimum||v>range.Maximum)throw new Exception("range value is outside element limits");((RangeValuePattern)pattern).SetValue(v);used="uia.range_value";}
     else throw Unsupported(background,"element is not editable");break;
    }
    case "perform_action":used=Action(Element(p),S(p,"action"),background);break;
    case "click":{
     string button=S(p,"button","left");int count=(int)N(p,"click_count",1);if(button=="")button="left";if(count==0)count=1;if(count<1||count>2)throw new Exception("invalid click count");
     if(p.ContainsKey("element_index")){
      var e=Element(p);object pattern;
      if(button=="left"&&count==1&&e.TryGetCurrentPattern(InvokePattern.Pattern,out pattern)){((InvokePattern)pattern).Invoke();used="uia.invoke";break;}
      if(background)throw new BackgroundError("element has no single semantic invoke; mouse fallback is forbidden");
      if(S(p,"mode")=="auto")Activate(h);
      var r=e.Current.BoundingRectangle;RECT wr=Rect(h);Position(h,r.X-wr.Left+r.Width/2,r.Y-wr.Top+r.Height/2);
     }else{if(background)throw new BackgroundError("coordinate click is forbidden");if(S(p,"mode")=="auto")Activate(h);Position(h,N(p,"x"),N(p,"y"));}
     uint down=button=="right"?8u:button=="middle"?32u:2u,up=button=="right"?16u:button=="middle"?64u:4u;
     for(int i=0;i<count;i++){Send(Mouse(down),Mouse(up));if(i+1<count)Thread.Sleep(60);}used="foreground_input.send_input";break;
    }
    case "press_key":Activate(h);Chord(S(p,"key"));used="foreground_input.send_input";break;
    case "type_text":Activate(h);foreach(char c in S(p,"text")){if(GetForegroundWindow()!=h)throw new Exception("focus changed during input; observe again before continuing");Send(Key(0,4,c),Key(0,6,c));}used="foreground_input.send_input";break;
    case "scroll":{
     double x=N(p,"x"),y=N(p,"y");
     if(p.ContainsKey("element_index")){
      object pattern;var e=Element(p);bool supported=e.TryGetCurrentPattern(ScrollPattern.Pattern,out pattern);
      if(supported){var scroll=(ScrollPattern)pattern;supported=(N(p,"delta_x")==0||scroll.Current.HorizontallyScrollable)&&(N(p,"delta_y")==0||scroll.Current.VerticallyScrollable);if(supported){scroll.Scroll(ScrollStep(N(p,"delta_x")),ScrollStep(N(p,"delta_y")));used="uia.scroll";break;}}
      if(S(p,"mode")!="auto")throw Unsupported(background,"element has no matching native scroll capability");
      var r=e.Current.BoundingRectangle;RECT wr=Rect(h);x=r.X-wr.Left+r.Width/2;y=r.Y-wr.Top+r.Height/2;
     }
     if(background)throw new BackgroundError("coordinate scroll is forbidden");if(S(p,"mode")=="auto")Activate(h);Position(h,x,y);int dx=(int)N(p,"delta_x"),dy=(int)N(p,"delta_y");Wheel(h,-dy,0x20A);Wheel(h,dx,0x20E);used="foreground_input.win32_wheel";break;
    }
    case "drag":{Position(h,N(p,"x"),N(p,"y"));RECT r=Rect(h);double tx=N(p,"to_x"),ty=N(p,"to_y");if(tx<0||ty<0||tx>=r.Right-r.Left||ty>=r.Bottom-r.Top)throw new Exception("drag destination outside window");Send(Mouse(2));try{for(int i=1;i<=12;i++){Position(h,N(p,"x")+(tx-N(p,"x"))*i/12,N(p,"y")+(ty-N(p,"y"))*i/12);Thread.Sleep(15);}}finally{Send(Mouse(4));}used="foreground_input.send_input";break;}
    default:throw new Exception("unsupported desktop operation");
   }
   return new {ok=true,operation_method=used,mode=ExecutionMode(p),foreground_fallback=S(p,"mode")=="auto"&&used.StartsWith("foreground_input.",StringComparison.Ordinal)&&(method=="click"||method=="scroll")&&p.ContainsKey("element_index")};
  }finally{Clear();}
 }
 static object Dispatch(string method,Dictionary<string,object> p){ValidateMode(method,p);switch(method){case "status":return Status();case "list_displays":Available();return new {displays=Displays()};case "screenshot":return CaptureScreenshot(p);case "list_windows":Available();return new {windows=ListWindows()};case "list_apps":Available();return new {apps=ListApps()};case "get_window_state":return Observe(p);case "request_permissions":return new {ok=true,message="Windows has no equivalent blanket grant; run in a logged-in desktop. Elevated and secure windows remain restricted."};default:return Operate(method,p);}}
 // UIA clients run on MTA; STA console clients can stall provider calls.
 [DllImport("kernel32.dll",SetLastError=true)] static extern bool GetNamedPipeServerProcessId(IntPtr pipe,out uint processId);
 static void RunRPC(TextReader input,TextWriter output){string line;while((line=input.ReadLine())!=null){if(line.Length>16*1024*1024)break;string id="";try{var request=Map(json.DeserializeObject(line));id=S(request,"id");object result=Dispatch(S(request,"method"),Map(request.ContainsKey("params")?request["params"]:null));output.WriteLine(json.Serialize(new {id=id,ok=true,result=result}));output.Flush();}catch(Exception e){Clear();output.WriteLine(json.Serialize(new {id=id,ok=false,error=e.Message,error_code=e is BackgroundError?"background_not_supported":""}));output.Flush();}}}
 [MTAThread] public static void Main(string[] args){
  try{
   try{SetProcessDpiAwarenessContext(new IntPtr(-4));}catch(EntryPointNotFoundException){SetProcessDPIAware();}
   if(args.Length==1&&args[0]=="--self-test"){
    ValidateMode("click",new Dictionary<string,object>{{"mode","auto"}});ValidateMode("perform_action",new Dictionary<string,object>{{"mode","auto"},{"action","focus"}});RequireScreenFallback(false);try{RequireScreenFallback(true);throw new Exception("background screen fallback accepted");}catch(BackgroundError){}
    bool fallback=false;string method;
    using(var image=ChooseBitmap(true,()=>new Bitmap(1,1),()=>{fallback=true;return new Bitmap(1,1);},out method)){if(fallback||method!="print_window")throw new Exception("native capture preference failed");}
    try{ChooseBitmap(true,()=>null,()=>{fallback=true;return new Bitmap(1,1);},out method);throw new Exception("strict capture unexpectedly succeeded");}catch(BackgroundError){if(fallback)throw new Exception("strict capture called screen fallback");}
    using(var image=ChooseBitmap(false,()=>null,()=>{fallback=true;return new Bitmap(1,1);},out method)){if(!fallback||method!="visible_screen")throw new Exception("default capture fallback failed");}
    Console.WriteLine("Windows desktop helper background capture policy self-test passed");return;
   }
   if(args.Length==0){Console.InputEncoding=new UTF8Encoding(false);Console.OutputEncoding=new UTF8Encoding(false);RunRPC(Console.In,Console.Out);return;}
   uint expected;
   if(args.Length!=4||args[0]!="--ipc-pipe"||args[2]!="--ipc-parent-pid"||!uint.TryParse(args[3],out expected)||expected==0||!args[1].StartsWith("gosshd-desktop-",StringComparison.Ordinal)){Environment.ExitCode=2;return;}
   // Exact data rights omit CreateNewInstance; no inherited service handles.
   using(var pipe=new NamedPipeClientStream(".",args[1],PipeAccessRights.ReadWrite|PipeAccessRights.Synchronize,PipeOptions.None,TokenImpersonationLevel.Identification,HandleInheritability.None)){
    pipe.Connect(15000);
    uint server;
    if(!GetNamedPipeServerProcessId(pipe.SafePipeHandle.DangerousGetHandle(),out server)||server!=expected)throw new IOException("desktop IPC server identity mismatch");
    using(var input=new StreamReader(pipe,new UTF8Encoding(false),false,65536))
    using(var output=new StreamWriter(pipe,new UTF8Encoding(false),65536)){output.AutoFlush=true;RunRPC(input,output);}
   }
  }catch{Environment.ExitCode=1;}
 }
}
