using System;using System.Windows.Forms;using System.Drawing;
class Fixture {
 [STAThread] static void Main(){
  var form=new Form{Text="GOSSHD desktop test fixture",Width=560,Height=380,StartPosition=FormStartPosition.CenterScreen,TopMost=true};
  var text=new TextBox{AccessibleName="Fixture text",Text="sample",Left=20,Top=20,Width=280};
  var label=new Label{Text="Not clicked",Left=20,Top=120,Width=480};
  var button=new Button{AccessibleName="Fixture button",Text="Apply",Left=20,Top=70,Width=120};
  var toggle=new CheckBox{AccessibleName="Fixture toggle",Text="Sample option",Left=330,Top=20,Width=150};
  var slider=new TrackBar{AccessibleName="Fixture slider",Minimum=0,Maximum=10,Value=3,Left=330,Top=60,Width=150};
  var choice=new ListBox{AccessibleName="Fixture choice",Left=330,Top=110,Width=150,Height=60};choice.Items.AddRange(new object[]{"First","Second"});choice.SelectedIndex=0;
  var scroll=new Panel{AccessibleName="Fixture scroll",AutoScroll=true,Left=20,Top=170,Width=260,Height=90};
  var scrollContent=new Label{Text="Scrollable sample",Top=0,Width=220,Height=900};scroll.Controls.Add(scrollContent);
  scrollContent.MouseWheel+=(s,e)=>{scroll.AutoScrollPosition=new Point(0,Math.Max(0,-scroll.AutoScrollPosition.Y-e.Delta));label.Text="Scrolled: "+e.Delta;};
  scroll.MouseWheel+=(s,e)=>{label.Text="Scrolled: "+e.Delta;};
  var drag=new Panel{AccessibleName="Fixture drag",Left=330,Top=220,Width=150,Height=90,BackColor=Color.LightBlue};
  bool dragging=false;drag.MouseDown+=(s,e)=>{dragging=true;};drag.MouseUp+=(s,e)=>{if(dragging)label.Text="Dragged";dragging=false;};
  text.KeyDown+=(s,e)=>{if(e.Control&&e.KeyCode==Keys.A){text.SelectAll();e.SuppressKeyPress=true;}};
  button.Click+=(s,e)=>{label.Text="Applied: "+text.Text;};
  form.Controls.AddRange(new Control[]{text,button,label,toggle,slider,choice,scroll,drag});Application.Run(form);
 }
}
