// Independent Windows JSON-lines helper. No GOSSHD/network dependencies.
using System;
using System.Collections;
using System.Collections.Generic;
using System.Diagnostics;
using System.Drawing;
using System.Drawing.Imaging;
using System.Globalization;
using System.IO;
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
 [DllImport("kernel32.dll")] static extern bool ProcessIdToSessionId(uint pid,out uint session);
 static Dictionary<string,object> Map(object o){return o as Dictionary<string,object> ?? new Dictionary<string,object>();}
 static string S(Dictionary<string,object> p,string k,string d=""){return p.ContainsKey(k)?Convert.ToString(p[k]):d;}
 static double N(Dictionary<string,object> p,string k,double d=0){return p.ContainsKey(k)?Convert.ToDouble(p[k]):d;}
 static bool B(Dictionary<string,object> p,string k,bool d=false){return p.ContainsKey(k)?Convert.ToBoolean(p[k]):d;}
 static object Window(IntPtr h){uint pid;GetWindowThreadProcessId(h,out pid);var t=new StringBuilder(513);GetWindowText(h,t,513);return new {id=h.ToInt64().ToString(),app=pid.ToString(),title=t.ToString()};}
 static IntPtr Target(Dictionary<string,object> p){var w=Map(p.ContainsKey("window")?p["window"]:null);long id;if(!long.TryParse(S(w,"id"),out id))throw new Exception("invalid window id");var h=new IntPtr(id);uint pid;GetWindowThreadProcessId(h,out pid);if(!IsWindow(h)||pid.ToString()!=S(w,"app"))throw new Exception("window no longer exists; enumerate again");return h;}
 static object Bounds(RECT r){return new {x=(double)r.Left,y=(double)r.Top,width=(double)(r.Right-r.Left),height=(double)(r.Bottom-r.Top)};}
 static RECT Rect(IntPtr h){RECT r;if(!GetWindowRect(h,out r)||r.Right<=r.Left||r.Bottom<=r.Top)throw new Exception("window has no visible bounds");return r;}
 static object ListWindows(){var list=new List<object>();EnumWindows(delegate(IntPtr h,IntPtr p){if(IsWindowVisible(h)){var t=new StringBuilder(513);GetWindowText(h,t,513);if(t.Length>0)list.Add(Window(h));}return list.Count<512;},IntPtr.Zero);return list;}
 static List<object> Displays(){var displays=new List<object>();EnumDisplayMonitors(IntPtr.Zero,IntPtr.Zero,delegate(IntPtr h,IntPtr dc,ref RECT rect,IntPtr data){var info=new MONITORINFO{cbSize=(uint)Marshal.SizeOf(typeof(MONITORINFO))};if(GetMonitorInfo(h,ref info))displays.Add(new {id=h.ToInt64().ToString(),bounds=Bounds(info.monitor),primary=(info.flags&1)!=0});return displays.Count<32;},IntPtr.Zero);return displays;}
 static RECT DisplayRect(string id){RECT result=new RECT();bool found=false;EnumDisplayMonitors(IntPtr.Zero,IntPtr.Zero,delegate(IntPtr h,IntPtr dc,ref RECT rect,IntPtr data){var info=new MONITORINFO{cbSize=(uint)Marshal.SizeOf(typeof(MONITORINFO))};if(GetMonitorInfo(h,ref info)&&((id==""&&(info.flags&1)!=0)||id==h.ToInt64().ToString())){result=info.monitor;found=true;return false;}return true;},IntPtr.Zero);if(!found)throw new Exception("display not found; enumerate again");return result;}
 static object ListApps(){var groups=new Dictionary<string,List<object>>();foreach(var w in (List<object>)ListWindows()){var m=Map(json.DeserializeObject(json.Serialize(w)));string app=S(m,"app");if(!groups.ContainsKey(app))groups[app]=new List<object>();groups[app].Add(w);}return groups.Select(x=>new {id=x.Key,windows=x.Value}).ToArray();}
 static void Available(){uint session;ProcessIdToSessionId((uint)Process.GetCurrentProcess().Id,out session);if(session==0)throw new Exception("no interactive desktop: run a desktop-enabled Agent in a logged-in user session, not Session 0");}
 static object Status(){uint session;ProcessIdToSessionId((uint)Process.GetCurrentProcess().Id,out session);return new {platform="windows",interactive=session!=0,permissions=new {screen_capture=session!=0,accessibility=session!=0},capture_backend="PrintWindow; visible-screen fallback",protocol_version=1};}
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
 static object Screenshot(IntPtr h,RECT r){int width=r.Right-r.Left,height=r.Bottom-r.Top;if(width>16384||height>16384||(long)width*height>64*1024*1024)throw new Exception("window exceeds capture limits");using(var bitmap=new Bitmap(width,height,PixelFormat.Format32bppArgb)){bool captured;using(var g=Graphics.FromImage(bitmap)){IntPtr dc=g.GetHdc();try{captured=h!=IntPtr.Zero&&PrintWindow(h,dc,2);}finally{g.ReleaseHdc(dc);}if(!captured)g.CopyFromScreen(r.Left,r.Top,0,0,new Size(width,height),CopyPixelOperation.SourceCopy);}
  double scale=Math.Min(1.0,1600.0/Math.Max(width,height));int w=Math.Max(1,(int)(width*scale)),hh=Math.Max(1,(int)(height*scale));using(var resized=new Bitmap(bitmap,w,hh))using(var ms=new MemoryStream()){resized.Save(ms,ImageFormat.Png);if(ms.Length>8*1024*1024)throw new Exception("screenshot exceeds size limit");return new {mime_type="image/png",data=Convert.ToBase64String(ms.ToArray()),width=w,height=hh};}}
 }
 static object CaptureScreenshot(Dictionary<string,object> p){Available();Clear();bool window=p.ContainsKey("window");var h=window?Target(p):IntPtr.Zero;var r=window?Rect(h):DisplayRect(S(p,"display_id"));return new {snapshot_id=Guid.NewGuid().ToString(),window=window?Window(h):new {id="",app=""},bounds=Bounds(r),image=Screenshot(h,r)};}
 static object Observe(Dictionary<string,object> p){Available();var h=Target(p);Clear();RECT r=Rect(h);var tree=new List<object>();bool truncated=false;if(B(p,"include_text")){int depth=(int)N(p,"max_depth",8),max=(int)N(p,"max_elements",512);if(depth<1||depth>16||max<1||max>1024)throw new Exception("invalid accessibility limits");Walk(AutomationElement.FromHandle(h),-1,0,depth,max,tree,r,ref truncated);}
  object image=B(p,"include_screenshot",true)?Screenshot(h,r):null;snapshot=Guid.NewGuid().ToString();observed=h;observedApp=S(Map(p["window"]),"app");observedRect=r;observedAt=DateTime.UtcNow;return new {snapshot_id=snapshot,window=Window(h),bounds=Bounds(r),elements=tree,image=image,truncated=truncated};}
 static void Send(params INPUT[] input){if(SendInput((uint)input.Length,input,Marshal.SizeOf(typeof(INPUT)))!=input.Length)throw new Exception("input injection rejected (possibly a higher-integrity or secure desktop)");}
 static INPUT Mouse(uint flag,uint data=0){return new INPUT{type=0,u=new InputUnion{mouse=new MOUSEINPUT{dwFlags=flag,mouseData=data}}};}
 static INPUT Key(ushort vk,uint flags=0,ushort scan=0){return new INPUT{type=1,u=new InputUnion{key=new KEYBDINPUT{vk=vk,flags=flags,scan=scan}}};}
 static void Position(IntPtr h,double x,double y){RECT r=Rect(h);if(x<0||y<0||x>=r.Right-r.Left||y>=r.Bottom-r.Top)throw new Exception("coordinate is outside the observed window");var point=new POINT{X=r.Left+(int)x,Y=r.Top+(int)y};if(GetAncestor(WindowFromPoint(point),2)!=h)throw new Exception("target point is occluded; no input was sent");if(!SetCursorPos(point.X,point.Y))throw new Exception("cannot position cursor");}
 static void Wheel(IntPtr h,int delta,uint message){if(delta==0)return;POINT point;if(!GetCursorPos(out point)||GetAncestor(WindowFromPoint(point),2)!=h)throw new Exception("scroll target changed; observe again");if(point.X<short.MinValue||point.X>short.MaxValue||point.Y<short.MinValue||point.Y>short.MaxValue)throw new Exception("wheel coordinates exceed native message bounds");var target=WindowFromPoint(point);uint coordinates=(uint)(ushort)point.X|((uint)(ushort)point.Y<<16);UIntPtr result;if(SendMessageTimeout(target,message,new UIntPtr(unchecked((uint)(delta<<16))),new IntPtr(unchecked((int)coordinates)),2,1000,out result)==IntPtr.Zero)throw new Exception("targeted wheel input was rejected or timed out");}
 static ushort KeyCode(string key){string k=key.ToLowerInvariant();switch(k){case "ctrl":case "control":case "control_l":return 0x11;case "alt":case "alt_l":return 0x12;case "shift":case "shift_l":return 0x10;case "meta":case "command":case "super_l":return 0x5B;case "return":case "enter":return 0x0D;case "tab":return 9;case "escape":case "esc":return 0x1B;case "space":return 0x20;case "backspace":return 8;case "delete":return 0x2E;case "left":return 0x25;case "up":return 0x26;case "right":return 0x27;case "down":return 0x28;case "home":return 0x24;case "end":return 0x23;case "pageup":case "page_up":return 0x21;case "pagedown":case "page_down":return 0x22;case "period":case ".":return 0xBE;case "comma":case ",":return 0xBC;case "minus":case "-":return 0xBD;}
  int f;if(k.StartsWith("f")&&int.TryParse(k.Substring(1),out f)&&f>=1&&f<=12)return (ushort)(0x6F+f);if(k.Length==1&&char.IsLetterOrDigit(k[0]))return (ushort)char.ToUpperInvariant(k[0]);throw new Exception("unsupported key name");}
 static void Chord(string chord){var keys=chord.Split('+').Select(x=>KeyCode(x.Trim())).ToArray();if(keys.Length<1||keys.Length>5)throw new Exception("invalid key chord");int pressed=0;try{foreach(var key in keys){Send(Key(key));pressed++;}}finally{for(int i=pressed-1;i>=0;i--)Send(Key(keys[i],2));}}
 static ScrollAmount ScrollStep(double delta){if(delta==0)return ScrollAmount.NoAmount;return delta>0?(delta>120?ScrollAmount.LargeIncrement:ScrollAmount.SmallIncrement):(delta< -120?ScrollAmount.LargeDecrement:ScrollAmount.SmallDecrement);}
 static void Action(AutomationElement e,string action){object p;switch(action){case "focus":e.SetFocus();return;case "invoke":if(e.TryGetCurrentPattern(InvokePattern.Pattern,out p)){((InvokePattern)p).Invoke();return;}break;case "toggle":if(e.TryGetCurrentPattern(TogglePattern.Pattern,out p)){((TogglePattern)p).Toggle();return;}break;case "expand":case "collapse":if(e.TryGetCurrentPattern(ExpandCollapsePattern.Pattern,out p)){if(action=="expand")((ExpandCollapsePattern)p).Expand();else ((ExpandCollapsePattern)p).Collapse();return;}break;case "select":if(e.TryGetCurrentPattern(SelectionItemPattern.Pattern,out p)){((SelectionItemPattern)p).Select();return;}break;}throw new Exception("accessibility action is not supported by this element");}
 static object Operate(string method,Dictionary<string,object> p){Available();if(method=="launch_app"){string app=S(p,"app");if(!Path.IsPathRooted(app)||!Path.GetFullPath(app).Equals(app,StringComparison.OrdinalIgnoreCase)||!app.EndsWith(".exe",StringComparison.OrdinalIgnoreCase)||!File.Exists(app))throw new Exception("launch_app requires an absolute existing .exe path");Clear();Process.Start(new ProcessStartInfo(app){UseShellExecute=true});return new {ok=true};}
  var h=ValidateSnapshot(p);// References stay private until this operation finishes.
  try{if(method=="activate_window"){Activate(h);return new {ok=true};}
   switch(method){case "set_value":{var e=Element(p);object vp;if(e.Current.IsPassword)throw new Exception("password elements are not exposed");if(e.TryGetCurrentPattern(ValuePattern.Pattern,out vp)&&!((ValuePattern)vp).Current.IsReadOnly){((ValuePattern)vp).SetValue(S(p,"text"));}else if(e.TryGetCurrentPattern(RangeValuePattern.Pattern,out vp)&&!((RangeValuePattern)vp).Current.IsReadOnly){double v;if(!double.TryParse(S(p,"text"),NumberStyles.Float,CultureInfo.InvariantCulture,out v)||double.IsNaN(v)||double.IsInfinity(v))throw new Exception("range value must be finite numeric text");var range=((RangeValuePattern)vp).Current;if(v<range.Minimum||v>range.Maximum)throw new Exception("range value is outside element limits");((RangeValuePattern)vp).SetValue(v);}else throw new Exception("element is not editable");break;}
   case "perform_action":Action(Element(p),S(p,"action"));break;
   case "click":{string button=S(p,"button","left");if(p.ContainsKey("element_index")){var e=Element(p);object ip;if(button=="left"&&e.TryGetCurrentPattern(InvokePattern.Pattern,out ip)){((InvokePattern)ip).Invoke();break;}var r=e.Current.BoundingRectangle;RECT wr=Rect(h);Position(h,r.X-wr.Left+r.Width/2,r.Y-wr.Top+r.Height/2);}else Position(h,N(p,"x"),N(p,"y"));uint down=button=="right"?8u:button=="middle"?32u:2u,up=button=="right"?16u:button=="middle"?64u:4u;int count=(int)N(p,"click_count",1);for(int i=0;i<count;i++){Send(Mouse(down),Mouse(up));if(i+1<count)Thread.Sleep(60);}break;}
   case "press_key":Activate(h);Chord(S(p,"key"));break;
   case "type_text":Activate(h);foreach(char c in S(p,"text")){if(GetForegroundWindow()!=h)throw new Exception("focus changed during input; observe again before continuing");Send(Key(0,4,c),Key(0,6,c));}break;
   case "scroll":if(p.ContainsKey("element_index")){object scroll;var e=Element(p);if(!e.TryGetCurrentPattern(ScrollPattern.Pattern,out scroll))throw new Exception("element has no native scroll pattern; use window-relative coordinates instead");((ScrollPattern)scroll).Scroll(ScrollStep(N(p,"delta_x")),ScrollStep(N(p,"delta_y")));break;}Position(h,N(p,"x"),N(p,"y"));int dx=(int)N(p,"delta_x"),dy=(int)N(p,"delta_y");Wheel(h,-dy,0x20A);Wheel(h,dx,0x20E);break;
   case "drag":{Position(h,N(p,"x"),N(p,"y"));RECT r=Rect(h);double tx=N(p,"to_x"),ty=N(p,"to_y");if(tx<0||ty<0||tx>=r.Right-r.Left||ty>=r.Bottom-r.Top)throw new Exception("drag destination outside window");Send(Mouse(2));try{for(int i=1;i<=12;i++){Position(h,N(p,"x")+(tx-N(p,"x"))*i/12,N(p,"y")+(ty-N(p,"y"))*i/12);Thread.Sleep(15);}}finally{Send(Mouse(4));}break;}
   default:throw new Exception("unsupported desktop operation");}return new {ok=true};
  }finally{Clear();}
 }
 static object Dispatch(string method,Dictionary<string,object> p){switch(method){case "status":return Status();case "list_displays":Available();return new {displays=Displays()};case "screenshot":return CaptureScreenshot(p);case "list_windows":Available();return new {windows=ListWindows()};case "list_apps":Available();return new {apps=ListApps()};case "get_window_state":return Observe(p);case "request_permissions":return new {ok=true,message="Windows has no equivalent blanket grant; run in a logged-in desktop. Elevated and secure windows remain restricted."};default:return Operate(method,p);}}
 // UIA clients run on MTA; STA console clients can stall provider calls.
 [MTAThread] public static void Main(string[] args){try{SetProcessDpiAwarenessContext(new IntPtr(-4));}catch(EntryPointNotFoundException){SetProcessDPIAware();}Console.InputEncoding=new UTF8Encoding(false);Console.OutputEncoding=new UTF8Encoding(false);string line;while((line=Console.ReadLine())!=null){if(line.Length>16*1024*1024)break;string id="";try{var request=Map(json.DeserializeObject(line));id=S(request,"id");object result=Dispatch(S(request,"method"),Map(request.ContainsKey("params")?request["params"]:null));Console.WriteLine(json.Serialize(new {id=id,ok=true,result=result}));}catch(Exception e){Clear();Console.WriteLine(json.Serialize(new {id=id,ok=false,error=e.Message}));}}}
}
