import AppKit
final class FixtureController:NSObject {
 let input=NSTextField(frame:NSRect(x:20,y:230,width:300,height:24))
 let result=NSTextField(labelWithString:"Not clicked")
 @objc func apply(_ sender:Any?) { result.stringValue="Applied: "+input.stringValue;result.setAccessibilityLabel(result.stringValue) }
}
let app=NSApplication.shared
app.setActivationPolicy(.regular)
let controller=FixtureController()
controller.input.stringValue="sample";controller.input.setAccessibilityLabel("Fixture text")
controller.result.frame=NSRect(x:20,y:130,width:400,height:24)
let button=NSButton(title:"Apply",target:controller,action:#selector(FixtureController.apply(_:)))
button.frame=NSRect(x:20,y:180,width:120,height:30);button.setAccessibilityLabel("Fixture button")
let window=NSWindow(contentRect:NSRect(x:0,y:0,width:540,height:320),styleMask:[.titled,.closable,.resizable],backing:.buffered,defer:false)
window.title="GOSSHD desktop test fixture";window.center();window.level = .floating
window.contentView?.addSubview(controller.input);window.contentView?.addSubview(button);window.contentView?.addSubview(controller.result)
window.makeKeyAndOrderFront(nil);app.activate(ignoringOtherApps:true)
app.run()
