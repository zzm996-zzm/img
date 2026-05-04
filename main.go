package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/google6409d0c57bc30ecb.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("google-site-verification: google6409d0c57bc30ecb.html"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	for _, fallbackPort := range []string{"8080", "8081"} {
		if fallbackPort == port {
			continue
		}
		go listenAndServe(mux, fallbackPort, false)
	}

	listenAndServe(mux, port, true)
}

func listenAndServe(handler http.Handler, port string, fatal bool) {
	log.Printf("Server listening on 0.0.0.0:%s", port)
	err := http.ListenAndServe(":"+port, handler)
	if fatal {
		log.Fatal(err)
	}
	log.Printf("Could not listen on fallback port %s: %v", port, err)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}

const indexHTML = `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>图片压缩 — 精确压缩到指定大小 | 免费在线工具</title>
<meta name="description" content="免费在线图片压缩，精确压缩到你指定的KB大小。支持JPG/PNG，无需注册，文件不上传服务器，完全在浏览器本地处理。">
<link href="https://fonts.googleapis.com/css2?family=Syne:wght@400;700;800&family=DM+Sans:wght@400;500&display=swap" rel="stylesheet">
<script async src="https://pagead2.googlesyndication.com/pagead/js/adsbygoogle.js?client=ca-pub-1902780696242483" crossorigin="anonymous"></script>
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
:root{
  --bg:#0e0e11;--surface:#18181d;--surface2:#222228;
  --border:rgba(255,255,255,0.07);--accent:#d4ff57;
  --accent-dim:rgba(212,255,87,0.1);--text:#f0f0f0;
  --muted:#777;--success:#57ff9a;--danger:#ff6b6b;
}
body{font-family:'DM Sans',sans-serif;background:var(--bg);color:var(--text);min-height:100vh;overflow-x:hidden}
body::before{content:'';position:fixed;inset:0;background-image:linear-gradient(rgba(255,255,255,0.02) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,0.02) 1px,transparent 1px);background-size:48px 48px;pointer-events:none;z-index:0}
.wrap{position:relative;z-index:1;max-width:700px;margin:0 auto;padding:64px 24px 80px}
.badge{display:inline-flex;align-items:center;gap:6px;background:var(--accent-dim);border:1px solid rgba(212,255,87,0.25);color:var(--accent);font-size:11px;font-weight:500;letter-spacing:.1em;text-transform:uppercase;padding:5px 12px;border-radius:20px;margin-bottom:20px}
.badge-dot{width:6px;height:6px;border-radius:50%;background:var(--accent);animation:pulse 2s ease-in-out infinite}
@keyframes pulse{0%,100%{opacity:1;transform:scale(1)}50%{opacity:.4;transform:scale(.7)}}
h1{font-family:'Syne',sans-serif;font-size:clamp(34px,6vw,54px);font-weight:800;line-height:1.05;letter-spacing:-.02em;margin-bottom:14px}
h1 em{color:var(--accent);font-style:normal}
.desc{color:var(--muted);font-size:15px;line-height:1.65;max-width:460px;margin-bottom:48px}

/* 免费次数提示 */
.quota-bar{display:flex;align-items:center;justify-content:space-between;background:var(--surface2);border:1px solid var(--border);border-radius:12px;padding:12px 16px;margin-bottom:20px;gap:12px}
.quota-left{font-size:13px;color:var(--muted)}
.quota-left strong{color:var(--text);font-family:'Syne',sans-serif}
.quota-track{flex:1;height:4px;background:rgba(255,255,255,0.06);border-radius:2px;overflow:hidden}
.quota-fill{height:100%;background:var(--accent);border-radius:2px;transition:width .4s}
.quota-fill.low{background:var(--danger)}
.quota-unlock{background:transparent;border:1px solid rgba(212,255,87,0.35);color:var(--accent);border-radius:8px;padding:7px 12px;font-size:12px;font-weight:700;cursor:pointer;white-space:nowrap}
.quota-unlock:hover{background:var(--accent-dim)}

.card{background:var(--surface);border:1px solid var(--border);border-radius:20px;padding:32px;margin-bottom:20px}
.drop-zone{border:1.5px dashed rgba(255,255,255,0.12);border-radius:14px;padding:52px 24px;text-align:center;cursor:pointer;transition:all .2s;background:var(--surface2);margin-bottom:24px}
.drop-zone:hover,.drop-zone.over{border-color:var(--accent);background:var(--accent-dim)}
.drop-zone.over{transform:scale(1.01)}
.drop-zone input{display:none}
.drop-icon{width:52px;height:52px;background:var(--surface);border:1px solid var(--border);border-radius:12px;display:flex;align-items:center;justify-content:center;margin:0 auto 14px;font-size:22px;transition:all .2s}
.drop-zone:hover .drop-icon{background:var(--accent-dim);border-color:rgba(212,255,87,0.3)}
.drop-title{font-family:'Syne',sans-serif;font-size:15px;font-weight:700;margin-bottom:5px}
.drop-title span{color:var(--accent)}
.drop-hint{font-size:12px;color:var(--muted)}
.preview{display:none;background:var(--surface2);border:1px solid var(--border);border-radius:12px;padding:14px 16px;margin-bottom:24px;align-items:center;gap:14px}
.preview.show{display:flex}
.prev-img{width:56px;height:56px;border-radius:8px;object-fit:cover;border:1px solid var(--border);flex-shrink:0}
.prev-info{flex:1;min-width:0}
.prev-name{font-size:13px;font-weight:500;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;margin-bottom:3px}
.prev-size{font-size:12px;color:var(--muted)}
.prev-change{font-size:12px;color:var(--muted);cursor:pointer;padding:6px 12px;border:1px solid var(--border);border-radius:8px;transition:all .15s;flex-shrink:0;white-space:nowrap}
.prev-change:hover{border-color:var(--accent);color:var(--accent)}
.field-label{font-size:11px;font-weight:500;color:var(--muted);text-transform:uppercase;letter-spacing:.08em;margin-bottom:10px}
.size-row{display:flex;align-items:center;gap:10px;margin-bottom:12px}
.size-input{flex:1;background:var(--surface2);border:1.5px solid var(--border);border-radius:10px;padding:12px 16px;font-size:26px;font-family:'Syne',sans-serif;font-weight:700;color:var(--text);outline:none;transition:border-color .2s;-moz-appearance:textfield}
.size-input::-webkit-outer-spin-button,.size-input::-webkit-inner-spin-button{-webkit-appearance:none}
.size-input:focus{border-color:var(--accent)}
.size-unit{font-family:'Syne',sans-serif;font-size:26px;font-weight:700;color:var(--muted)}
.presets{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:28px}
.preset{padding:6px 14px;background:var(--surface2);border:1px solid var(--border);border-radius:8px;color:var(--muted);font-size:13px;font-weight:500;cursor:pointer;transition:all .15s}
.preset:hover,.preset.on{border-color:var(--accent);color:var(--accent);background:var(--accent-dim)}
.btn{width:100%;padding:16px;background:var(--accent);color:#0e0e11;border:none;border-radius:12px;font-size:16px;font-family:'Syne',sans-serif;font-weight:700;cursor:pointer;transition:all .2s}
.btn:hover:not(:disabled){background:#c8f53f;transform:translateY(-1px)}
.btn:active:not(:disabled){transform:translateY(0)}
.btn:disabled{background:var(--surface2);color:var(--muted);cursor:not-allowed;border:1px solid var(--border)}
.btn.pay{background:linear-gradient(135deg,#f59e0b,#ef4444);color:#fff}
.btn.pay:hover:not(:disabled){background:linear-gradient(135deg,#d97706,#dc2626);transform:translateY(-1px)}
.status{display:none;margin-top:14px;padding:13px 16px;border-radius:10px;font-size:13px;font-weight:500;align-items:center;gap:10px}
.status.show{display:flex}
.status.loading{background:rgba(255,255,255,0.03);border:1px solid var(--border);color:var(--muted)}
.status.ok{background:rgba(87,255,154,0.07);border:1px solid rgba(87,255,154,0.25);color:var(--success)}
.status.err{background:rgba(255,107,107,0.07);border:1px solid rgba(255,107,107,0.25);color:var(--danger)}
.sdot{width:7px;height:7px;border-radius:50%;flex-shrink:0}
.loading .sdot{background:var(--muted);animation:pulse 1s ease-in-out infinite}
.ok .sdot{background:var(--success)}
.err .sdot{background:var(--danger)}
@keyframes spin{to{transform:rotate(360deg)}}
.spinner{width:14px;height:14px;border:2px solid rgba(255,255,255,0.08);border-top-color:var(--muted);border-radius:50%;animation:spin .7s linear infinite;flex-shrink:0}

/* paywall modal */
.overlay{display:none;position:fixed;inset:0;background:rgba(0,0,0,0.8);z-index:100;align-items:center;justify-content:center;padding:24px}
.overlay.show{display:flex}
.modal{background:var(--surface);border:1px solid var(--border);border-radius:20px;padding:40px 32px;max-width:420px;width:100%;text-align:center}
.modal-icon{font-size:48px;margin-bottom:16px}
.modal-title{font-family:'Syne',sans-serif;font-size:24px;font-weight:800;margin-bottom:8px}
.modal-desc{color:var(--muted);font-size:14px;line-height:1.6;margin-bottom:28px}
.modal-price{font-family:'Syne',sans-serif;font-size:42px;font-weight:800;color:var(--accent);margin-bottom:4px}
.modal-price-desc{font-size:13px;color:var(--muted);margin-bottom:28px}
.modal-features{text-align:left;background:var(--surface2);border-radius:12px;padding:16px 20px;margin-bottom:24px}
.modal-feature{font-size:13px;color:var(--text);padding:5px 0;display:flex;align-items:center;gap:10px}
.modal-feature::before{content:'✓';color:var(--accent);font-weight:700;flex-shrink:0}
.modal-close{margin-top:16px;font-size:13px;color:var(--muted);cursor:pointer;text-decoration:underline}
.modal-close:hover{color:var(--text)}

.features{display:grid;grid-template-columns:repeat(3,1fr);gap:12px}
@media(max-width:500px){.features{grid-template-columns:1fr}}
.feat{background:var(--surface);border:1px solid var(--border);border-radius:14px;padding:20px}
.feat-icon{font-size:20px;margin-bottom:10px}
.feat-title{font-family:'Syne',sans-serif;font-size:13px;font-weight:700;margin-bottom:4px}
.feat-desc{font-size:12px;color:var(--muted);line-height:1.55}
.faq{margin-top:48px}
.faq-title{font-family:'Syne',sans-serif;font-size:22px;font-weight:800;margin-bottom:24px;letter-spacing:-.01em}
.faq-item{border-bottom:1px solid var(--border);padding:20px 0;cursor:pointer}
.faq-item:last-child{border-bottom:none}
.faq-q{font-family:'Syne',sans-serif;font-size:15px;font-weight:700;display:flex;justify-content:space-between;align-items:center;gap:16px}
.faq-arrow{color:var(--muted);font-size:18px;flex-shrink:0;transition:transform .2s}
.faq-item.open .faq-arrow{transform:rotate(45deg)}
.faq-a{font-size:14px;color:var(--muted);line-height:1.75;max-height:0;overflow:hidden;transition:max-height .3s ease,padding .3s}
.faq-item.open .faq-a{max-height:300px;padding-top:12px}
@keyframes up{from{opacity:0;transform:translateY(18px)}to{opacity:1;transform:translateY(0)}}
.header{animation:up .5s ease both}
.card{animation:up .5s ease .08s both}
.features{animation:up .5s ease .16s both}
</style>
</head>
<body>
<div class="wrap">
  <div class="header">
    <div class="badge"><span class="badge-dot"></span>免费工具</div>
    <h1>图片压缩<br><em>精确到你要的大小</em></h1>
    <p class="desc">填一个目标KB数，在你的浏览器本地完成压缩。图片不上传服务器，完全私密。</p>
  </div>

  <!-- 免费次数进度条 -->
  <div class="quota-bar" id="quotaBar">
    <div class="quota-left">免费次数：<strong id="quotaText">100 / 100</strong></div>
    <div class="quota-track"><div class="quota-fill" id="quotaFill" style="width:100%"></div></div>
    <button class="quota-unlock" onclick="showPaywall()">PayPal 解锁</button>
  </div>

  <div class="card">
    <div class="drop-zone" id="dz">
      <input type="file" id="fi" accept="image/jpeg,image/png,image/webp">
      <div class="drop-icon">🖼</div>
      <div class="drop-title">拖拽图片到这里，或<span>点击选择</span></div>
      <div class="drop-hint">JPG · PNG · WebP &nbsp;·&nbsp; 本地处理，不上传服务器</div>
    </div>

    <div class="preview" id="prev">
      <img class="prev-img" id="pimg" src="" alt="">
      <div class="prev-info">
        <div class="prev-name" id="pname"></div>
        <div class="prev-size" id="psize"></div>
      </div>
      <div class="prev-change" onclick="document.getElementById('fi').click()">换一张</div>
    </div>

    <div class="field-label">目标文件大小</div>
    <div class="size-row">
      <input class="size-input" type="number" id="tgt" value="200" min="10" max="20000">
      <span class="size-unit">KB</span>
    </div>
    <div class="presets">
      <button class="preset" onclick="setTarget(100)">100 KB</button>
      <button class="preset on" onclick="setTarget(200)">200 KB</button>
      <button class="preset" onclick="setTarget(500)">500 KB</button>
      <button class="preset" onclick="setTarget(1024)">1 MB</button>
      <button class="preset" onclick="setTarget(2048)">2 MB</button>
    </div>

    <button class="btn" id="btn" onclick="compress()" disabled>选择图片后开始</button>
    <div class="status" id="st"></div>
  </div>

  <div class="features">
    <div class="feat"><div class="feat-icon">⚡</div><div class="feat-title">浏览器本地处理</div><div class="feat-desc">图片不上传服务器，速度快，完全私密</div></div>
    <div class="feat"><div class="feat-icon">🎯</div><div class="feat-title">精确压缩</div><div class="feat-desc">二分法算法，紧贴目标大小，不会超限</div></div>
    <div class="feat"><div class="feat-icon">🆓</div><div class="feat-title">100次免费</div><div class="feat-desc">每位用户免费100次，之后$10解锁无限使用</div></div>
  </div>

  <div class="faq">
    <div class="faq-title">常见问题</div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">如何把图片压缩到200KB以内？<span class="faq-arrow">+</span></div>
      <div class="faq-a">上传图片后，在目标大小输入框填写"200"，点击"开始压缩"，工具会自动将图片压缩到200KB以内并下载。适合政府表单、招聘网站等对图片大小有严格限制的场景。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">图片压缩后会不会很模糊？<span class="faq-arrow">+</span></div>
      <div class="faq-a">我们使用二分法算法，在满足目标大小的前提下尽量保留最高画质。目标大小设置越接近原图大小，画质损失越小。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">支持压缩到1MB、500KB、100KB吗？<span class="faq-arrow">+</span></div>
      <div class="faq-a">支持任意目标大小，可以直接点击预设按钮，也可以手动输入任意数值。无论压缩到1MB还是50KB都可以处理。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">图片会上传到服务器保存吗？<span class="faq-arrow">+</span></div>
      <div class="faq-a">不会。所有压缩操作完全在你的浏览器本地完成，图片数据从不离开你的设备，完全私密安全。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">支持哪些图片格式？PNG能压缩吗？<span class="faq-arrow">+</span></div>
      <div class="faq-a">支持 JPG、PNG、WebP 格式。PNG 图片会自动转换为 JPG 格式进行压缩，压缩效果更好，文件体积更小。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">微信发图片太模糊，怎么办？<span class="faq-arrow">+</span></div>
      <div class="faq-a">微信会对超过5MB的图片进行二次压缩。建议将图片压缩到4MB以内再发送，在目标大小输入"4096"即可有效避免微信自动压缩。</div>
    </div>
    <div class="faq-item" onclick="this.classList.toggle('open')">
      <div class="faq-q">免费次数用完了怎么办？<span class="faq-arrow">+</span></div>
      <div class="faq-a">免费额度用完后，支付$10即可解锁无限次使用，一次付费永久有效，不收月费。</div>
    </div>
  </div>
</div>

<!-- Paywall Modal -->
<div class="overlay" id="overlay">
  <div class="modal">
    <div class="modal-icon">🎉</div>
    <div class="modal-title">已用完免费次数</div>
    <div class="modal-desc">你已完成100次免费压缩。支付一次，永久无限使用。</div>
    <div class="modal-price">$10</div>
    <div class="modal-price-desc">一次付费 · 永久有效 · 无月费</div>
    <div class="modal-features">
      <div class="modal-feature">无限次图片压缩</div>
      <div class="modal-feature">精确压缩到任意指定大小</div>
      <div class="modal-feature">浏览器本地处理，完全私密</div>
      <div class="modal-feature">支持 JPG / PNG / WebP</div>
    </div>
    <button class="btn pay" onclick="goPay()">使用 PayPal 解锁 $10 →</button>
    <div class="modal-close" onclick="closeModal()">暂时不需要</div>
  </div>
</div>

<script>
const FREE_LIMIT = 100;
const STORAGE_KEY = 'img_compress_count';
const PAYPAL_PAYMENT_LINK = 'https://www.paypal.com/ncp/payment/LN55VSJYNE252';

let f = null;
let usedCount = parseInt(localStorage.getItem(STORAGE_KEY) || '0');

const dz = document.getElementById('dz');
const fi = document.getElementById('fi');
const prev = document.getElementById('prev');
const pimg = document.getElementById('pimg');
const pname = document.getElementById('pname');
const psize = document.getElementById('psize');
const btn = document.getElementById('btn');
const st = document.getElementById('st');

updateQuota();

dz.onclick = () => fi.click();
dz.ondragover = e => { e.preventDefault(); dz.classList.add('over'); };
dz.ondragleave = () => dz.classList.remove('over');
dz.ondrop = e => { e.preventDefault(); dz.classList.remove('over'); if (e.dataTransfer.files[0]) load(e.dataTransfer.files[0]); };
fi.onchange = e => { if (e.target.files[0]) load(e.target.files[0]); };

function load(file) {
  if (!file.type.startsWith('image/')) { showStatus('err', '请选择图片文件'); return; }
  f = file;
  const r = new FileReader();
  r.onload = e => {
    pimg.src = e.target.result;
    pname.textContent = file.name;
    psize.textContent = '原始大小：' + (file.size / 1024).toFixed(1) + ' KB';
    prev.classList.add('show');
    dz.style.display = 'none';
  };
  r.readAsDataURL(file);
  btn.disabled = false;
  btn.textContent = '开始压缩 →';
  st.className = 'status';
}

function setTarget(kb) {
  document.getElementById('tgt').value = kb;
  document.querySelectorAll('.preset').forEach(b => {
    const val = parseInt(b.textContent);
    b.classList.toggle('on', val === kb || (kb === 1024 && b.textContent.includes('1 MB')) || (kb === 2048 && b.textContent.includes('2 MB')));
  });
}

function updateQuota() {
  const remaining = Math.max(0, FREE_LIMIT - usedCount);
  const pct = (remaining / FREE_LIMIT) * 100;
  document.getElementById('quotaText').textContent = remaining + ' / ' + FREE_LIMIT;
  const fill = document.getElementById('quotaFill');
  fill.style.width = pct + '%';
  fill.className = 'quota-fill' + (remaining <= 10 ? ' low' : '');
}

async function compress() {
  if (!f) return;

  // 检查免费次数
  if (usedCount >= FREE_LIMIT) {
    showPaywall();
    return;
  }

  const targetKB = parseFloat(document.getElementById('tgt').value);
  if (!targetKB || targetKB <= 0) { showStatus('err', '请输入有效的目标大小'); return; }
  const targetBytes = targetKB * 1024;

  btn.disabled = true;
  btn.textContent = '压缩中...';
  showStatus('loading', '正在压缩，请稍候...');

  try {
    const result = await compressInBrowser(f, targetBytes);
    const origKB = (f.size / 1024).toFixed(1);
    const resultKB = (result.size / 1024).toFixed(1);
    const saved = (((f.size - result.size) / f.size) * 100).toFixed(0);

    // 下载
    const url = URL.createObjectURL(result);
    const a = document.createElement('a');
    const ext = result.type === 'image/png' ? '.png' : '.jpg';
    const base = f.name.replace(/\.[^.]+$/, '');
    a.download = 'compressed_' + base + ext;
    a.href = url;
    a.click();
    URL.revokeObjectURL(url);

    // 记录次数
    usedCount++;
    localStorage.setItem(STORAGE_KEY, usedCount);
    updateQuota();

    showStatus('ok', origKB + ' KB → ' + resultKB + ' KB · 减少 ' + saved + '% · 已自动下载');
    psize.textContent = '原始：' + origKB + ' KB → 压缩后：' + resultKB + ' KB';
  } catch (e) {
    showStatus('err', '压缩失败，请重试');
  } finally {
    btn.disabled = false;
    btn.textContent = '再次压缩 →';
  }
}

// 浏览器端二分法压缩
function compressInBrowser(file, targetBytes) {
  return new Promise((resolve, reject) => {
    const img = new Image();
    const url = URL.createObjectURL(file);
    img.onload = () => {
      URL.revokeObjectURL(url);
      const canvas = document.createElement('canvas');
      canvas.width = img.naturalWidth;
      canvas.height = img.naturalHeight;
      const ctx = canvas.getContext('2d');
      ctx.drawImage(img, 0, 0);

      // 如果原图已经小于目标，直接返回
      if (file.size <= targetBytes) {
        canvas.toBlob(blob => resolve(blob), 'image/jpeg', 0.95);
        return;
      }

      // 二分法找最优 quality
      let low = 0.01, high = 0.95, bestBlob = null, attempts = 0;

      function tryQuality(q) {
        canvas.toBlob(blob => {
          attempts++;
          if (!blob) { reject(new Error('Failed')); return; }

          if (blob.size <= targetBytes) {
            bestBlob = blob;
            low = q;
          } else {
            high = q;
          }

          if (attempts >= 10 || (high - low) < 0.01) {
            if (bestBlob) {
              resolve(bestBlob);
            } else {
              // 还是太大，用最低质量
              canvas.toBlob(b => resolve(b), 'image/jpeg', 0.01);
            }
            return;
          }
          tryQuality((low + high) / 2);
        }, 'image/jpeg', q);
      }

      tryQuality((low + high) / 2);
    };
    img.onerror = reject;
    img.src = url;
  });
}

function showPaywall() {
  document.getElementById('overlay').classList.add('show');
}

function closeModal() {
  document.getElementById('overlay').classList.remove('show');
}

function goPay() {
  window.location.href = PAYPAL_PAYMENT_LINK;
}

function showStatus(type, msg) {
  st.className = 'status show ' + type;
  if (type === 'loading') {
    st.innerHTML = '<div class="spinner"></div><span>' + msg + '</span>';
  } else {
    st.innerHTML = '<div class="sdot"></div><span>' + msg + '</span>';
  }
}
</script>
</body>
</html>`
