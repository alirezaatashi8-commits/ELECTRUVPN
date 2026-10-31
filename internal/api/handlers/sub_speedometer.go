package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/xray"
	"github.com/gin-gonic/gin"
)

// PublicLive returns the realtime speed of the caller's own subscription.
//
// It is reachable with the subscription token only (the same secret that
// already grants the config links and usage numbers) and exposes nothing but
// the client's own speeds and a device count - never IP addresses.
func (h *SubscriptionHandler) PublicLive(c *gin.Context) {
	subID := strings.TrimSpace(c.Param("subId"))
	var cl models.Client
	if subID == "" || h.S.DB.Where("sub_id = ?", subID).First(&cl).Error != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "not found"})
		return
	}
	if !cl.SubscriptionEnabled || strings.EqualFold(cl.SubscriptionStatus, "revoked") {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "msg": "subscription unavailable"})
		return
	}
	h.S.RefreshLive()
	live, _ := xray.LiveSampleForSubjects(clientTrafficSubjects(h.S.DB, cl)...)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{
		"online":        live.Online,
		"downloadSpeed": live.DownlinkSpeed, // bytes per second, server -> user
		"uploadSpeed":   live.UplinkSpeed,   // bytes per second, user -> server
		"devices":       live.Devices,
		"connections":   live.Connections,
		"ts":            time.Now().UnixMilli(),
	}})
}

// subSpeedCSS / subSpeedHTML / subSpeedJS are spliced into the subscription
// landing page (subscriptionLandingHTML). They contain no backticks so they can
// live in raw strings.
const subSpeedCSS = `
.speed-grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}
.gauge{border-radius:22px;padding:12px 8px 14px;border:1px solid var(--stroke);
  background:radial-gradient(120% 90% at 50% 0%,var(--surface2),var(--soft));text-align:center;position:relative}
.gauge svg{width:100%;max-width:300px;height:auto;display:block;direction:ltr;margin:0 auto}
.gauge .g-title{font-size:.84rem;font-weight:800;margin-bottom:2px}
.gauge .g-title.dl{color:var(--ink-violet)}.gauge .g-title.ul{color:var(--ink-cyan)}
.gauge .g-val{font-weight:800;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;fill:var(--text)}
.gauge .g-unit{fill:var(--muted);font-size:9.5px;font-weight:700}
.gauge .g-lbl{fill:var(--muted);font-size:6.6px;font-weight:700}
.gauge .g-needle{filter:drop-shadow(0 0 3px rgba(139,92,246,.6))}
.gauge .g-needle path{fill:var(--text)}
.gauge .g-hub{fill:var(--hub);stroke:var(--text);stroke-width:2}
.gauge .g-tick{stroke:var(--tick);stroke-width:1.4}
.gauge .g-tick2{stroke:var(--tick2);stroke-width:1}
.speed-meta{display:flex;justify-content:space-between;align-items:center;gap:8px;margin-top:14px;flex-wrap:wrap}
.speed-meta .chip{font-family:inherit;font-size:.78rem;color:var(--text)}
.speed-meta .chip b{font-weight:800}
.speed-meta .chip .i{color:var(--accent)}
.hint{font-size:.74rem;color:var(--muted);margin-top:10px;line-height:1.6}
@media (max-width:560px){.speed-grid{grid-template-columns:1fr}}
`

const subSpeedHTML = `
  <section class="card reveal c-speed">
    <div class="card-head">
      <span class="ico"><svg class="i"><use href="#i-bolt"/></svg></span>
      <div><h2>سرعت لحظه‌ای</h2><p class="muted">دانلود و آپلود همین الان شما</p></div>
      <span class="pill off" id="spState">در حال بررسی…</span>
    </div>
    <div class="speed-grid">
      <div class="gauge"><div class="g-title dl">↓ دانلود</div><svg id="gaugeDown" viewBox="0 0 200 168" role="img" aria-label="سرعت دانلود"></svg></div>
      <div class="gauge"><div class="g-title ul">↑ آپلود</div><svg id="gaugeUp" viewBox="0 0 200 168" role="img" aria-label="سرعت آپلود"></svg></div>
    </div>
    <div class="speed-meta">
      <span class="chip"><svg class="i"><use href="#i-devices"/></svg>دستگاه متصل: <b id="spDevices">0</b></span>
      <span class="chip" id="spConnWrap" style="display:none"><svg class="i"><use href="#i-link"/></svg>اتصال فعال: <b id="spConns">0</b></span>
    </div>
    <p class="hint">سرعت از سمت سرور اندازه‌گیری می‌شود و هر چند ثانیه به‌روز می‌شود. وقتی از VPN استفاده نمی‌کنید عدد صفر است.</p>
  </section>
`

const subSpeedJS = `
(function(){
  var NS='http://www.w3.org/2000/svg';
  var CX=100, CY=98, R=76, A0=-120, A1=120, SEG=48;
  // Speedtest-style scale: equal angle per step, speed in bytes/s.
  var MARKS=[0,131072,524288,1048576,2097152,5242880,10485760,20971520];
  var LABELS=['0','128K','512K','1M','2M','5M','10M','20M'];
  function el(n,a,p){var e=document.createElementNS(NS,n);for(var k in a)e.setAttribute(k,a[k]);if(p)p.appendChild(e);return e}
  function pt(a,r){var t=a*Math.PI/180;return [CX+r*Math.sin(t),CY-r*Math.cos(t)]}
  function arc(a0,a1,r){var p=pt(a0,r),q=pt(a1,r);return 'M'+p[0].toFixed(2)+' '+p[1].toFixed(2)+' A'+r+' '+r+' 0 '+((a1-a0)>180?1:0)+' 1 '+q[0].toFixed(2)+' '+q[1].toFixed(2)}
  function angOf(v){
    var n=MARKS.length-1; if(!(v>0))return A0; if(v>=MARKS[n])return A1;
    for(var i=0;i<n;i++){ if(v<=MARKS[i+1]){ return A0+((i+(v-MARKS[i])/(MARKS[i+1]-MARKS[i]))/n)*(A1-A0); } }
    return A1;
  }
  // low speed = red, high speed = green
  function colorAt(f){return 'hsl('+Math.round(Math.max(0,Math.min(1,f))*125)+',88%,52%)'}
  function fmt(b){
    if(!(b>0))return {v:'0.0',u:'KB/s'};
    if(b<1048576){var k=b/1024;return {v:k>=100?k.toFixed(0):k.toFixed(1),u:'KB/s'}}
    var m=b/1048576;return {v:m>=10?m.toFixed(1):m.toFixed(2),u:'MB/s'}
  }
  function Gauge(svg){
    var g={svg:svg,segs:[],a:A0,v:0,ta:A0,tv:0};
    for(var i=0;i<SEG;i++){
      var a=A0+(A1-A0)*i/SEG, b=A0+(A1-A0)*(i+1)/SEG;
      var s=el('path',{d:arc(a,b+0.6,R),fill:'none','stroke-width':9,stroke:colorAt((i+0.5)/SEG),'stroke-linecap':'butt',opacity:0.22},svg);
      s._end=b; g.segs.push(s);
    }
    for(var j=0;j<MARKS.length;j++){
      var aj=A0+(A1-A0)*j/(MARKS.length-1), p1=pt(aj,R-7), p2=pt(aj,R-13), pl=pt(aj,R-23);
      el('line',{x1:p1[0],y1:p1[1],x2:p2[0],y2:p2[1],'class':'g-tick'},svg);
      var t=el('text',{x:pl[0],y:pl[1]+2.2,'text-anchor':'middle','class':'g-lbl'},svg); t.textContent=LABELS[j];
    }
    for(var m=0;m<MARKS.length-1;m++){
      var am=A0+(A1-A0)*(m+0.5)/(MARKS.length-1), q1=pt(am,R-7), q2=pt(am,R-10);
      el('line',{x1:q1[0],y1:q1[1],x2:q2[0],y2:q2[1],'class':'g-tick2'},svg);
    }
    g.needle=el('g',{'class':'g-needle'},svg);
    el('path',{d:'M'+CX+' '+(CY-R+8)+' L'+(CX+3.6)+' '+CY+' L'+(CX-3.6)+' '+CY+' Z',},g.needle);
    el('circle',{cx:CX,cy:CY,r:7.5,'class':'g-hub'},svg);
    g.val=el('text',{x:CX,y:CY+34,'text-anchor':'middle','class':'g-val','font-size':26},svg);
    g.unit=el('text',{x:CX,y:CY+49,'text-anchor':'middle','class':'g-unit'},svg);
    g.val.textContent='0.0'; g.unit.textContent='KB/s';
    g.set=function(bytes){g.tv=bytes>0?bytes:0;g.ta=angOf(g.tv)};
    g.draw=function(){
      g.needle.setAttribute('transform','rotate('+g.a.toFixed(2)+' '+CX+' '+CY+')');
      for(var i=0;i<g.segs.length;i++){g.segs[i].setAttribute('opacity',g.segs[i]._end<=g.a+0.01?'1':'0.22')}
      var f=fmt(g.v); g.val.textContent=f.v; g.unit.textContent=f.u;
      if(g.v>0){g.val.style.fill=colorAt((g.a-A0)/(A1-A0))}else{g.val.style.fill=''}
    };
    g.step=function(){
      var da=g.ta-g.a, dv=g.tv-g.v;
      if(Math.abs(da)<0.05&&Math.abs(dv)<1){g.a=g.ta;g.v=g.tv;return false}
      g.a+=da*0.14; g.v+=dv*0.16; return true;
    };
    g.draw();
    return g;
  }
  var down=Gauge(document.getElementById('gaugeDown')), up=Gauge(document.getElementById('gaugeUp'));
  var raf=0;
  function loop(){
    var a=down.step(), b=up.step();
    down.draw(); up.draw();
    raf=(a||b)?requestAnimationFrame(loop):0;
  }
  function kick(){ if(!raf) raf=requestAnimationFrame(loop); }

  var stateEl=document.getElementById('spState'), devEl=document.getElementById('spDevices'),
      connEl=document.getElementById('spConns'), connWrap=document.getElementById('spConnWrap');
  function setState(online,known){
    stateEl.className='pill'+(online?'':' off');
    stateEl.textContent=known?(online?'● آنلاین':'○ آفلاین'):'نامشخص';
  }
  var url=location.pathname.replace(/\/+$/,'')+'/live', busy=false, fails=0;
  function poll(){
    if(busy||document.hidden)return; busy=true;
    fetch(url,{cache:'no-store',credentials:'same-origin'}).then(function(r){return r.ok?r.json():Promise.reject(r.status)}).then(function(d){
      var o=(d&&d.obj)||{}; fails=0;
      down.set(Number(o.downloadSpeed)||0); up.set(Number(o.uploadSpeed)||0); kick();
      devEl.textContent=String(o.devices||0);
      var c=Number(o.connections)||0; connEl.textContent=String(c); connWrap.style.display=c>0?'':'none';
      setState(!!o.online,true);
    }).catch(function(){
      fails++; if(fails>=2){down.set(0);up.set(0);kick();setState(false,false)}
    }).then(function(){busy=false});
  }
  poll(); setInterval(poll,2000);
  document.addEventListener('visibilitychange',function(){if(!document.hidden)poll()});
})();
`
