// Independent macOS JSON-lines helper. Requires macOS 12.3 or later.
import AppKit
import ApplicationServices
import ScreenCaptureKit
import CoreImage
import CoreMedia

struct DesktopError: Error { let message: String; init(_ message: String) { self.message = message } }
let imageContext = CIContext()
var snapshot = ""
var observedWindow: [String: Any] = [:]
var observedBounds = CGRect.zero
var observedAt = Date.distantPast
var elements: [AXUIElement] = []
func text(_ p: [String: Any], _ key: String, _ defaultValue: String = "") -> String { p[key] as? String ?? defaultValue }
func number(_ p: [String: Any], _ key: String, _ defaultValue: Double = 0) -> Double { (p[key] as? NSNumber)?.doubleValue ?? defaultValue }
func boolean(_ p: [String: Any], _ key: String, _ defaultValue: Bool = false) -> Bool { p[key] as? Bool ?? defaultValue }
func clearObservation() { snapshot = ""; elements.removeAll(); observedWindow = [:] }
func rectJSON(_ r: CGRect) -> [String: Any] { ["x":r.origin.x,"y":r.origin.y,"width":r.width,"height":r.height] }
func windowInfos() -> [[String: Any]] { CGWindowListCopyWindowInfo([.optionOnScreenOnly,.excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? [] }
func bounds(_ info: [String: Any]) -> CGRect { if let data = info[kCGWindowBounds as String] as? [String:Any], let r = CGRect(dictionaryRepresentation:data as CFDictionary) { return r }; return .zero }
func windowObject(_ info: [String: Any]) -> [String: Any] { ["id":String((info[kCGWindowNumber as String] as? NSNumber)?.uint32Value ?? 0),"app":String((info[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value ?? 0),"title":String((info[kCGWindowName as String] as? String ?? "").prefix(512))] }
func target(_ p: [String: Any]) throws -> [String: Any] { guard let w = p["window"] as? [String: Any], let info = windowInfos().first(where: { let actual = windowObject($0); return text(actual,"id") == text(w,"id") && text(actual,"app") == text(w,"app") }) else { throw DesktopError("window no longer exists; enumerate again") }; return info }
func interactive() -> Bool {
 guard getuid() != 0,let session=CGSessionCopyCurrentDictionary() as? [String:Any] else {return false}
 return (session[kCGSessionOnConsoleKey as String] as? Bool ?? false) && (session[kCGSessionLoginDoneKey as String] as? Bool ?? false) && !(session["CGSSessionScreenIsLocked"] as? Bool ?? false)
}
func status() -> [String: Any] { ["platform":"darwin","interactive":interactive(),"permissions":["screen_capture":CGPreflightScreenCaptureAccess(),"accessibility":AXIsProcessTrusted()],"capture_backend":"ScreenCaptureKit","protocol_version":1] }
func requireDesktop() throws { if !interactive() { throw DesktopError("no logged-in user desktop; run the helper in a user's graphical session, not a root LaunchDaemon") } }
func requireAX() throws { if !AXIsProcessTrusted() { throw DesktopError("accessibility permission required: enable GOSSHD Desktop Helper in System Settings > Privacy & Security > Accessibility") } }
func attribute(_ e: AXUIElement, _ name: String) -> CFTypeRef? { var result: CFTypeRef?; return AXUIElementCopyAttributeValue(e,name as CFString,&result) == .success ? result : nil }
func valueString(_ value:CFTypeRef?) -> String { if let text=value as? String {return String(text.prefix(4096))};if let number=value as? NSNumber{return number.stringValue};return "" }
func stringAttribute(_ e: AXUIElement, _ name: String) -> String { let value = attribute(e,name); return valueString(value) }
func axRect(_ e: AXUIElement) -> CGRect { var point = CGPoint.zero; var size = CGSize.zero; if let p = attribute(e,kAXPositionAttribute), CFGetTypeID(p) == AXValueGetTypeID() { _ = AXValueGetValue(p as! AXValue,.cgPoint,&point) }; if let s = attribute(e,kAXSizeAttribute), CFGetTypeID(s) == AXValueGetTypeID() { _ = AXValueGetValue(s as! AXValue,.cgSize,&size) }; return CGRect(origin:point,size:size) }
func settable(_ e: AXUIElement, _ name: String) -> Bool { var value = DarwinBoolean(false); return AXUIElementIsAttributeSettable(e,name as CFString,&value) == .success && value.boolValue }
func nativeActions(_ e: AXUIElement) -> [String] { var names: CFArray?; _ = AXUIElementCopyActionNames(e,&names); return names as? [String] ?? [] }
func actions(_ e: AXUIElement) -> [String] { var result:[String] = []; let names = nativeActions(e); if names.contains(kAXPressAction) { result += ["invoke","toggle"] }; if names.contains(kAXShowMenuAction) { result.append("secondary") }; if names.contains(where:{$0.hasPrefix("AXScroll")&&$0.hasSuffix("ByPage")}) {result.append("scroll")};if settable(e,kAXValueAttribute) { result.append("set_value") }; if settable(e,kAXFocusedAttribute) { result.append("focus") }; if settable(e,kAXSelectedAttribute) { result.append("select") }; if settable(e,kAXExpandedAttribute) { result += ["expand","collapse"] }; return result }
func axWindow(_ info: [String: Any]) throws -> AXUIElement { try requireAX(); let pid = (info[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value ?? 0; let app = AXUIElementCreateApplication(pid); let windows = attribute(app,kAXWindowsAttribute) as? [AXUIElement] ?? []; let r = bounds(info); let matches = windows.filter { let a = axRect($0); return abs(a.minX-r.minX)<4 && abs(a.minY-r.minY)<4 && abs(a.width-r.width)<4 && abs(a.height-r.height)<4 }; if matches.count == 1 { return matches[0] }; throw DesktopError("cannot uniquely match this window to an accessibility window") }
func walk(_ e: AXUIElement, _ parent: Int, _ depth: Int, _ maxDepth: Int, _ maxElements: Int, _ origin: CGRect, _ tree: inout [[String: Any]], _ truncated: inout Bool) {
 if tree.count >= maxElements { truncated = true; return }
 let role = stringAttribute(e,kAXRoleAttribute)
 if role == "AXSecureTextField" || stringAttribute(e,kAXSubroleAttribute) == "AXSecureTextField" { return }
 let index = elements.count; elements.append(e); let r = axRect(e)
 tree.append(["index":index,"parent":parent,"role":role,"name":stringAttribute(e,kAXTitleAttribute).isEmpty ? stringAttribute(e,kAXDescriptionAttribute) : stringAttribute(e,kAXTitleAttribute),"value":stringAttribute(e,kAXValueAttribute),"automation_id":stringAttribute(e,"AXIdentifier"),"help":String(stringAttribute(e,kAXHelpAttribute).prefix(1024)),"focused":attribute(e,kAXFocusedAttribute) as? Bool ?? false,"selected":attribute(e,kAXSelectedAttribute) as? Bool ?? NSNull(),"expanded":attribute(e,kAXExpandedAttribute) as? Bool ?? NSNull(),"minimum":attribute(e,"AXMinValue") as? NSNumber ?? NSNull(),"maximum":attribute(e,"AXMaxValue") as? NSNumber ?? NSNull(),"enabled":attribute(e,kAXEnabledAttribute) as? Bool ?? true,"bounds":rectJSON(r.offsetBy(dx:-origin.minX,dy:-origin.minY)),"actions":actions(e)])
 let children = attribute(e,kAXChildrenAttribute) as? [AXUIElement] ?? []
 if depth >= maxDepth { if !children.isEmpty { truncated = true }; return }
 for child in children { if tree.count >= maxElements { truncated = true; break }; walk(child,index,depth+1,maxDepth,maxElements,origin,&tree,&truncated) }
}

final class Capture: NSObject, SCStreamOutput, SCStreamDelegate {
 let completed = DispatchSemaphore(value:0)
 let lock = NSLock()
 var data:Data?; var error:Error?; var size:CGSize = .zero
 func finish(_ image:Data?, _ size:CGSize, _ error:Error?) { lock.lock(); defer { lock.unlock() }; if data != nil || self.error != nil { return }; data=image; self.size=size; self.error=error; completed.signal() }
 func stream(_ stream: SCStream, didStopWithError error: Error) { finish(nil,.zero,error) }
 func stream(_ stream: SCStream, didOutputSampleBuffer sampleBuffer: CMSampleBuffer, of type: SCStreamOutputType) {
  guard type == .screen, sampleBuffer.isValid, let buffer = CMSampleBufferGetImageBuffer(sampleBuffer) else { return }
  if let attachments = CMSampleBufferGetSampleAttachmentsArray(sampleBuffer,createIfNecessary:false) as? [[SCStreamFrameInfo:Any]], let frameStatus = attachments.first?[.status] as? Int, frameStatus != SCFrameStatus.complete.rawValue { return }
  let image = CIImage(cvPixelBuffer:buffer)
  guard let cg = imageContext.createCGImage(image,from:image.extent), let png = NSBitmapImageRep(cgImage:cg).representation(using:.png,properties:[:]) else { finish(nil,.zero,DesktopError("cannot encode screenshot")); return }
  finish(png,CGSize(width:cg.width,height:cg.height),nil)
 }
}
func displays() -> [[String:Any]] {var ids=[CGDirectDisplayID](repeating:0,count:32);var count:UInt32=0;guard CGGetActiveDisplayList(32,&ids,&count) == .success else{return []};return ids.prefix(Int(count)).map{["id":String($0),"bounds":rectJSON(CGDisplayBounds($0)),"primary":$0==CGMainDisplayID()]} }
func screenshot(_ info: [String: Any], _ displayID:String="") throws -> [String: Any] {
 if !CGPreflightScreenCaptureAccess() { throw DesktopError("screen recording permission required: enable GOSSHD Desktop Helper in System Settings > Privacy & Security > Screen Recording") }
 let contentReady=DispatchSemaphore(value:0); var content:SCShareableContent?; var contentError:Error?
 SCShareableContent.getExcludingDesktopWindows(false,onScreenWindowsOnly:true) { value,error in content=value;contentError=error;contentReady.signal() }
 if contentReady.wait(timeout:.now()+10) != .success { throw DesktopError("screen content enumeration timed out") }
 if let error=contentError { throw error }

 let filter:SCContentFilter;let r:CGRect
 if !info.isEmpty {
  let id=(info[kCGWindowNumber as String] as? NSNumber)?.uint32Value ?? 0
  guard let window=content?.windows.first(where:{$0.windowID==id}) else {throw DesktopError("window is not capturable")}
  filter=SCContentFilter(desktopIndependentWindow:window);r=bounds(info)
 } else {
  let id=displayID.isEmpty ? CGMainDisplayID():UInt32(displayID) ?? 0
  guard let display=content?.displays.first(where:{$0.displayID==id}) else {throw DesktopError("display not found; enumerate again")}
  filter=SCContentFilter(display:display,excludingApplications:[],exceptingWindows:[]);r=CGDisplayBounds(id)
 }
 let scale=min(1,1600/max(r.width,r.height));let configuration=SCStreamConfiguration()
 configuration.width=max(1,Int(r.width*scale));configuration.height=max(1,Int(r.height*scale));configuration.queueDepth=1;configuration.minimumFrameInterval=CMTime(value:1,timescale:5);configuration.showsCursor=false
 let capture=Capture(),stream=SCStream(filter:filter,configuration:configuration,delegate:capture)
 try stream.addStreamOutput(capture,type:.screen,sampleHandlerQueue:DispatchQueue(label:"desktop.capture"))
 stream.startCapture { error in if let error=error { capture.finish(nil,.zero,error) } }
 defer { stream.stopCapture { _ in } }
 if capture.completed.wait(timeout:.now()+10) != .success { throw DesktopError("screenshot timed out") }
 if let error=capture.error { throw error }
 guard let data=capture.data,data.count<=8*1024*1024 else { throw DesktopError("screenshot exceeds size limit") }
 return ["mime_type":"image/png","data":data.base64EncodedString(),"width":Int(capture.size.width),"height":Int(capture.size.height)]
}
func observe(_ p: [String: Any]) throws -> [String: Any] {
 try requireDesktop(); let info=try target(p);clearObservation();let r=bounds(info)
 if r.isEmpty || r.width*r.height>64*1024*1024 { throw DesktopError("invalid or oversized window") }
 var tree:[[String:Any]]=[],truncated=false
 if boolean(p,"include_text") { let depth=Int(number(p,"max_depth",8)),max=Int(number(p,"max_elements",512)); if depth<1 || depth>16 || max<1 || max>1024 { throw DesktopError("invalid accessibility limits") }; let root=try axWindow(info);walk(root,-1,0,depth,max,r,&tree,&truncated) }
 var result:[String:Any]=["window":windowObject(info),"bounds":rectJSON(r),"elements":tree,"truncated":truncated]
 if boolean(p,"include_screenshot",true) { result["image"]=try screenshot(info) }
 snapshot=UUID().uuidString;observedWindow=windowObject(info);observedBounds=r;observedAt=Date();result["snapshot_id"]=snapshot;return result
}
func validSnapshot(_ p:[String:Any]) throws -> [String:Any] { let info=try target(p),w=windowObject(info),r=bounds(info); if snapshot.isEmpty || snapshot != text(p,"snapshot_id") || text(w,"id") != text(observedWindow,"id") || text(w,"app") != text(observedWindow,"app") || Date().timeIntervalSince(observedAt)>60 || abs(r.width-observedBounds.width)>1 || abs(r.height-observedBounds.height)>1 { throw DesktopError("stale snapshot; observe this window again") };return info }
func activate(_ info:[String:Any]) throws { try requireAX();let pid=(info[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value ?? 0;guard let app=NSRunningApplication(processIdentifier:pid),app.activate(options:.activateIgnoringOtherApps) else { throw DesktopError("cannot activate target application") };Thread.sleep(forTimeInterval:0.1);if NSWorkspace.shared.frontmostApplication?.processIdentifier != pid { throw DesktopError("target did not become active; input was not sent") };let window=try axWindow(info);let error=AXUIElementPerformAction(window,kAXRaiseAction as CFString);if error != .success && error != .actionUnsupported { throw DesktopError("cannot raise target window") } }
func element(_ p:[String:Any]) throws -> AXUIElement { let i=Int(number(p,"element_index",-1));if i<0 || i>=elements.count { throw DesktopError("element_index is not in the latest observation") };return elements[i] }
func setAttribute(_ e:AXUIElement,_ name:String,_ value:CFTypeRef) throws { if !settable(e,name) || AXUIElementSetAttributeValue(e,name as CFString,value) != .success { throw DesktopError("accessibility attribute is not editable") } }
func perform(_ e:AXUIElement,_ action:String) throws { switch action {case "focus":try setAttribute(e,kAXFocusedAttribute,kCFBooleanTrue);case "select":try setAttribute(e,kAXSelectedAttribute,kCFBooleanTrue);case "expand":try setAttribute(e,kAXExpandedAttribute,kCFBooleanTrue);case "collapse":try setAttribute(e,kAXExpandedAttribute,kCFBooleanFalse);case "invoke","toggle","secondary":let native=action=="secondary" ? kAXShowMenuAction:kAXPressAction;if !nativeActions(e).contains(native) || AXUIElementPerformAction(e,native as CFString) != .success { throw DesktopError("accessibility action is not supported") };default:throw DesktopError("unsupported accessibility action") } }
func point(_ info:[String:Any],_ x:Double,_ y:Double) throws -> CGPoint { let r=bounds(info);if !x.isFinite || !y.isFinite || x<0 || y<0 || x>=r.width || y>=r.height { throw DesktopError("coordinate outside the observed window") };return CGPoint(x:r.minX+x,y:r.minY+y) }
func mouse(_ type:CGEventType,_ point:CGPoint,_ button:CGMouseButton = .left,_ count:Int = 1) throws { guard let e=CGEvent(mouseEventSource:nil,mouseType:type,mouseCursorPosition:point,mouseButton:button) else { throw DesktopError("cannot create mouse event") };e.setIntegerValueField(.mouseEventClickState,value:Int64(count));e.post(tap:.cghidEventTap) }
let keyCodes:[String:CGKeyCode] = ["a":0,"s":1,"d":2,"f":3,"h":4,"g":5,"z":6,"x":7,"c":8,"v":9,"b":11,"q":12,"w":13,"e":14,"r":15,"y":16,"t":17,"1":18,"2":19,"3":20,"4":21,"6":22,"5":23,"equal":24,"9":25,"7":26,"minus":27,"8":28,"0":29,"o":31,"u":32,"i":34,"p":35,"return":36,"enter":36,"l":37,"j":38,"k":40,"semicolon":41,"comma":43,"slash":44,"n":45,"m":46,"period":47,"tab":48,"space":49,"backspace":51,"escape":53,"esc":53,"delete":117,"home":115,"end":119,"pageup":116,"page_up":116,"pagedown":121,"page_down":121,"left":123,"right":124,"down":125,"up":126,"f1":122,"f2":120,"f3":99,"f4":118,"f5":96,"f6":97,"f7":98,"f8":100,"f9":101,"f10":109,"f11":103,"f12":111]
func chord(_ text:String) throws { var flags:CGEventFlags=[];var key:CGKeyCode?;let parts=text.lowercased().split(separator:"+").map{String($0).trimmingCharacters(in:.whitespaces)};if parts.isEmpty || parts.count>5 { throw DesktopError("invalid key chord") };for part in parts { switch part {case "ctrl","control","control_l":flags.insert(.maskControl);case "alt","option","alt_l":flags.insert(.maskAlternate);case "shift","shift_l":flags.insert(.maskShift);case "meta","command","cmd","super_l":flags.insert(.maskCommand);default:guard key==nil,let code=keyCodes[part] else { throw DesktopError("unsupported key name") };key=code} };guard let code=key,let down=CGEvent(keyboardEventSource:nil,virtualKey:code,keyDown:true),let up=CGEvent(keyboardEventSource:nil,virtualKey:code,keyDown:false) else { throw DesktopError("key chord needs a non-modifier key") };down.flags=flags;up.flags=[];down.post(tap:.cghidEventTap);up.post(tap:.cghidEventTap) }
func unicodeChunks(_ units:[UInt16]) -> [[UInt16]] {
 var chunks:[[UInt16]]=[];var start=0
 while start<units.count {
  // CGEvent Unicode payloads are limited to 20 UTF-16 units.
  var end=min(start+20,units.count)
  if end<units.count && (0xD800...0xDBFF).contains(units[end-1]) {end-=1}
  chunks.append(Array(units[start..<end]));start=end
 }
 return chunks
}
func typeText(_ text:String) throws { let units=Array(text.utf16);if units.count>16384 { throw DesktopError("text too large") };for chunk in unicodeChunks(units){guard let down=CGEvent(keyboardEventSource:nil,virtualKey:0,keyDown:true),let up=CGEvent(keyboardEventSource:nil,virtualKey:0,keyDown:false) else { throw DesktopError("cannot create keyboard event") };chunk.withUnsafeBufferPointer { down.keyboardSetUnicodeString(stringLength:chunk.count,unicodeString:$0.baseAddress);up.keyboardSetUnicodeString(stringLength:chunk.count,unicodeString:$0.baseAddress) };down.flags=[];up.flags=[];down.post(tap:.cghidEventTap);up.post(tap:.cghidEventTap) } }
func operate(_ method:String,_ p:[String:Any]) throws -> [String:Any] {
 try requireDesktop();try requireAX()
 if method=="launch_app" { let path=text(p,"app");if !path.hasPrefix("/") || !path.hasSuffix(".app") || !FileManager.default.fileExists(atPath:path) { throw DesktopError("launch_app needs an absolute existing .app path") };clearObservation();if !NSWorkspace.shared.open(URL(fileURLWithPath:path)) { throw DesktopError("cannot launch app") };return ["ok":true] }
 let info=try validSnapshot(p);defer { clearObservation() };try activate(info)
 switch method {
 case "activate_window":break
 case "set_value":let e=try element(p);if stringAttribute(e,kAXSubroleAttribute)=="AXSecureTextField" { throw DesktopError("password elements are not exposed") };if let minimum=attribute(e,"AXMinValue") as? NSNumber,let maximum=attribute(e,"AXMaxValue") as? NSNumber {guard let value=Double(text(p,"text")),value.isFinite,value>=minimum.doubleValue,value<=maximum.doubleValue else {throw DesktopError("range value is outside element limits")};try setAttribute(e,kAXValueAttribute,NSNumber(value:value))}else{try setAttribute(e,kAXValueAttribute,text(p,"text") as CFString)}
 case "perform_action":try perform(element(p),text(p,"action"))
 case "press_key":try chord(text(p,"key"))
 case "type_text":try typeText(text(p,"text"))
 case "click":var pos:CGPoint;let button=text(p,"button","left"),count=Int(number(p,"click_count",1));if p["element_index"] != nil { let e=try element(p);if button=="left" && count==1 && nativeActions(e).contains(kAXPressAction) { try perform(e,"invoke");break };let r=axRect(e);pos=try point(info,r.midX-bounds(info).minX,r.midY-bounds(info).minY) } else { pos=try point(info,number(p,"x"),number(p,"y")) };let mb:CGMouseButton=button=="right" ? .right:button=="middle" ? .center:.left;let down:CGEventType=button=="right" ? .rightMouseDown:button=="middle" ? .otherMouseDown:.leftMouseDown;let up:CGEventType=button=="right" ? .rightMouseUp:button=="middle" ? .otherMouseUp:.leftMouseUp;if count<1 || count>2 { throw DesktopError("invalid click count") };for i in 1...count { try mouse(down,pos,mb,i);try mouse(up,pos,mb,i) }
 case "scroll":if p["element_index"] != nil {let e=try element(p);let dx=number(p,"delta_x"),dy=number(p,"delta_y");let requested=[dx>0 ? "AXScrollRightByPage":dx<0 ? "AXScrollLeftByPage":"",dy>0 ? "AXScrollDownByPage":dy<0 ? "AXScrollUpByPage":""].filter{!$0.isEmpty};for action in requested {if !nativeActions(e).contains(action)||AXUIElementPerformAction(e,action as CFString) != .success {throw DesktopError("element has no matching native scroll action; use window-relative coordinates instead")}};break};let pos=try point(info,number(p,"x"),number(p,"y"));try mouse(.mouseMoved,pos);guard let event=CGEvent(scrollWheelEvent2Source:nil,units:.pixel,wheelCount:2,wheel1:Int32(-number(p,"delta_y")),wheel2:Int32(number(p,"delta_x")),wheel3:0) else { throw DesktopError("cannot create scroll event") };event.post(tap:.cghidEventTap)
 case "drag":let start=try point(info,number(p,"x"),number(p,"y")),end=try point(info,number(p,"to_x"),number(p,"to_y"));try mouse(.leftMouseDown,start);defer { try? mouse(.leftMouseUp,end) };for i in 1...12 { let f=Double(i)/12;try mouse(.leftMouseDragged,CGPoint(x:start.x+(end.x-start.x)*f,y:start.y+(end.y-start.y)*f));Thread.sleep(forTimeInterval:0.015) }
 default:throw DesktopError("unsupported desktop method") }
 return ["ok":true]
}
func dispatch(_ method:String,_ p:[String:Any]) throws -> Any {
 switch method {
 case "status":return status()
 case "list_displays":try requireDesktop();return ["displays":displays()]
 case "screenshot":try requireDesktop();clearObservation();if p["window"] != nil {let info=try target(p);return ["snapshot_id":UUID().uuidString,"window":windowObject(info),"bounds":rectJSON(bounds(info)),"image":try screenshot(info)] as [String:Any]};let id=text(p,"display_id");let number=id.isEmpty ? CGMainDisplayID():UInt32(id) ?? 0;return ["snapshot_id":UUID().uuidString,"window":["id":"","app":""],"bounds":rectJSON(CGDisplayBounds(number)),"image":try screenshot([:],id)] as [String:Any]
 case "list_windows":try requireDesktop();return ["windows":windowInfos().filter { (($0[kCGWindowLayer as String] as? NSNumber)?.intValue ?? 0)==0 }.prefix(512).map(windowObject)]
 case "list_apps":try requireDesktop();let windows=windowInfos().filter { (($0[kCGWindowLayer as String] as? NSNumber)?.intValue ?? 0)==0 }.prefix(512).map(windowObject);let groups=Dictionary(grouping:windows,by:{text($0,"app")});return ["apps":groups.map{["id":$0.key,"windows":$0.value] as [String:Any]}]
 case "get_window_state":return try observe(p)
 case "request_permissions":try requireDesktop();_ = CGRequestScreenCaptureAccess();let options=[kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String:true] as CFDictionary;_ = AXIsProcessTrustedWithOptions(options);clearObservation();return status()
 default:return try operate(method,p)
 }
}
if CommandLine.arguments.contains("--self-test") {
 do { try chord("unsupported-key") ; fatalError("invalid key accepted") } catch {}
 let units=Array((String(repeating:"x",count:19)+"🙂 中文").utf16)
 let chunks=unicodeChunks(units)
 if chunks.flatMap({$0}) != units || chunks.contains(where:{!$0.isEmpty && (0xD800...0xDBFF).contains($0.last!)}) {fatalError("Unicode surrogate pair split")}
 let r=CGRect(x:10,y:20,width:100,height:200)
 if (rectJSON(r)["width"] as? CGFloat) != 100 { fatalError("bounds encoding failed") }
 print("macOS desktop helper self-test passed")
} else {
 while let line=readLine() {
  if line.utf8.count>16*1024*1024 { break };var id=""
  do { guard let data=line.data(using:.utf8),let request=try JSONSerialization.jsonObject(with:data) as? [String:Any] else { throw DesktopError("invalid JSON request") };id=text(request,"id");let result=try dispatch(text(request,"method"),request["params"] as? [String:Any] ?? [:]);let response:[String:Any]=["id":id,"ok":true,"result":result];let output=try JSONSerialization.data(withJSONObject:response,options:[.sortedKeys]);if output.count>16*1024*1024 { throw DesktopError("desktop response exceeds limit") };FileHandle.standardOutput.write(output);FileHandle.standardOutput.write(Data([10]))
  } catch { clearObservation();let message=(error as? DesktopError)?.message ?? error.localizedDescription;let output=try! JSONSerialization.data(withJSONObject:["id":id,"ok":false,"error":message] as [String:Any]);FileHandle.standardOutput.write(output);FileHandle.standardOutput.write(Data([10])) }
 }
}
