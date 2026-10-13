import AppKit
if let path=ProcessInfo.processInfo.environment["GOSSHD_FIXTURE_PID_FILE"] {try! String(getpid()).write(toFile:path,atomically:true,encoding:.utf8)}
final class PointerFixture:NSView {
 let result=NSTextField(labelWithString:"Pointer ready")
 var scrollCount=0
 override var acceptsFirstResponder:Bool {true}
 override func acceptsFirstMouse(for event:NSEvent?)->Bool {true}
 override init(frame:NSRect) {
  super.init(frame:frame)
  setAccessibilityElement(true);setAccessibilityRole(.group);setAccessibilityLabel("Fixture pointer")
  result.frame=NSRect(x:10,y:10,width:320,height:24);addSubview(result)
  wantsLayer=true;layer?.backgroundColor=NSColor.controlBackgroundColor.cgColor
 }
 required init?(coder:NSCoder) {fatalError("not used")}
 override func mouseDown(with event:NSEvent) {window?.makeFirstResponder(self)}
 override func mouseDragged(with event:NSEvent) {result.stringValue="Dragged";result.setAccessibilityLabel("Dragged")}
 override func scrollWheel(with event:NSEvent) {scrollCount+=1;result.stringValue="Scrolled: \(scrollCount)";result.setAccessibilityLabel(result.stringValue)}
}
final class FixtureController:NSObject,NSApplicationDelegate {
 var window:NSWindow?
 let input=NSTextField(frame:NSRect(x:20,y:430,width:420,height:24))
 let result=NSTextField(labelWithString:"Not clicked")
 @objc func apply(_ sender:Any?) {result.stringValue="Applied: "+input.stringValue;result.setAccessibilityLabel(result.stringValue)}
 func applicationDidFinishLaunching(_ notification:Notification){
  input.stringValue="sample";input.setAccessibilityLabel("Fixture text")
  result.frame=NSRect(x:20,y:330,width:500,height:24)
  let button=NSButton(title:"Apply",target:self,action:#selector(apply(_:)))
  button.frame=NSRect(x:20,y:380,width:120,height:30);button.setAccessibilityLabel("Fixture button")
  let toggle=NSButton(checkboxWithTitle:"Fixture toggle",target:nil,action:nil)
  toggle.frame=NSRect(x:20,y:275,width:200,height:24)
  let slider=NSSlider(value:2,minValue:0,maxValue:10,target:nil,action:nil)
  slider.frame=NSRect(x:20,y:220,width:300,height:24);slider.setAccessibilityLabel("Fixture slider")
  let pointer=PointerFixture(frame:NSRect(x:20,y:30,width:420,height:140))
  let created=NSWindow(contentRect:NSRect(x:0,y:0,width:680,height:520),styleMask:[.titled,.closable,.resizable],backing:.buffered,defer:false)
  created.title="GOSSHD desktop test fixture";created.center();created.level = .normal
  created.isReleasedWhenClosed=false
  for view in [input,button,result,toggle,slider,pointer] as [NSView] {created.contentView?.addSubview(view)}
  window=created;created.makeKeyAndOrderFront(nil);NSApplication.shared.activate(ignoringOtherApps:true)
 }
}
let app=NSApplication.shared
app.setActivationPolicy(.regular)
let mainMenu=NSMenu()
let editItem=NSMenuItem(title:"Edit",action:nil,keyEquivalent:"")
let editMenu=NSMenu(title:"Edit")
editMenu.addItem(withTitle:"Select All",action:#selector(NSText.selectAll(_:)),keyEquivalent:"a")
editItem.submenu=editMenu;mainMenu.addItem(editItem);app.mainMenu=mainMenu
let controller=FixtureController()
app.delegate=controller
withExtendedLifetime(controller){app.run()}
