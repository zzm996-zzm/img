package main

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/compress", handleCompress)
	mux.HandleFunc("/google6409d0c57bc30ecb.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("google-site-verification: google6409d0c57bc30ecb.html"))
	})
	port := "8081"
	log.Printf("Server running at http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func handleCompress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		http.Error(w, "File too large (max 20MB)", http.StatusBadRequest)
		return
	}
	targetKBStr := r.FormValue("target_kb")
	targetKB, err := strconv.ParseFloat(targetKBStr, 64)
	if err != nil || targetKB <= 0 {
		http.Error(w, "Invalid target size", http.StatusBadRequest)
		return
	}
	targetBytes := int(targetKB * 1024)
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Failed to read image", http.StatusBadRequest)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read image data", http.StatusInternalServerError)
		return
	}
	if len(data) <= targetBytes {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="compressed_%s"`, header.Filename))
		w.Header().Set("Content-Type", header.Header.Get("Content-Type"))
		w.Write(data)
		return
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		http.Error(w, "Unsupported image format", http.StatusBadRequest)
		return
	}
	result, finalFormat, err := compressToTarget(img, format, targetBytes)
	if err != nil {
		http.Error(w, "Compression failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	ext := "." + finalFormat
	baseName := strings.TrimSuffix(header.Filename, "."+format)
	outName := fmt.Sprintf("compressed_%s%s", baseName, ext)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, outName))
	w.Header().Set("Content-Type", "image/"+finalFormat)
	w.Header().Set("Content-Length", strconv.Itoa(len(result)))
	w.Write(result)
}

func compressToTarget(img image.Image, format string, targetBytes int) ([]byte, string, error) {
	outFormat := format
	if format == "png" {
		outFormat = "jpeg"
	}
	if outFormat == "jpeg" || outFormat == "jpg" {
		return compressJPEG(img, targetBytes)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "png", nil
}

func compressJPEG(img image.Image, targetBytes int) ([]byte, string, error) {
	low, high := 1, 95
	var bestResult []byte
	for low <= high {
		mid := (low + high) / 2
		var buf bytes.Buffer
		err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: mid})
		if err != nil {
			return nil, "", err
		}
		if buf.Len() <= targetBytes {
			bestResult = buf.Bytes()
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	if bestResult == nil {
		var buf bytes.Buffer
		jpeg.Encode(&buf, img, &jpeg.Options{Quality: 1})
		return buf.Bytes(), "jpeg", nil
	}
	return bestResult, "jpeg", nil
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
<title>图片压缩 — 精确压缩到指定大小</title>
<meta name="description" content="免费在线图片压缩，精确压缩到你指定的KB大小。支持JPG/PNG，无需注册，文件不保存。">
<link href="https://fonts.googleapis.com/css2?family=Syne:wght@400;700;800&family=DM+Sans:wght@400;500&display=swap" rel="stylesheet">
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
.btn{width:100%;padding:16px;background:var(--accent);color:#0e0e11;border:none;border-radius:12px;font-size:16px;font-family:'Syne',sans-serif;font-weight:700;cursor:pointer;transition:all .2s;letter-spacing:.01em}
.btn:hover:not(:disabled){background:#c8f53f;transform:translateY(-1px)}
.btn:active:not(:disabled){transform:translateY(0)}
.btn:disabled{background:var(--surface2);color:var(--muted);cursor:not-allowed;border:1px solid var(--border)}
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
.features{display:grid;grid-template-columns:repeat(3,1fr);gap:12px}
@media(max-width:500px){.features{grid-template-columns:1fr}}
.feat{background:var(--surface);border:1px solid var(--border);border-radius:14px;padding:20px}
.feat-icon{font-size:20px;margin-bottom:10px}
.feat-title{font-family:'Syne',sans-serif;font-size:13px;font-weight:700;margin-bottom:4px}
.feat-desc{font-size:12px;color:var(--muted);line-height:1.55}
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
    <p class="desc">填一个目标KB数，自动压缩到位。政府表单、邮件附件、微信上传，再也不超限。</p>
  </div>

  <div class="card">
    <div class="drop-zone" id="dz">
      <input type="file" id="fi" accept="image/jpeg,image/png,image/webp">
      <div class="drop-icon">🖼</div>
      <div class="drop-title">拖拽图片到这里，或<span>点击选择</span></div>
      <div class="drop-hint">JPG · PNG · WebP &nbsp;·&nbsp; 最大 20MB</div>
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
    <div class="presets" id="presets">
      <button class="preset" onclick="set(100)">100 KB</button>
      <button class="preset on" onclick="set(200)">200 KB</button>
      <button class="preset" onclick="set(500)">500 KB</button>
      <button class="preset" onclick="set(1024)">1 MB</button>
      <button class="preset" onclick="set(2048)">2 MB</button>
    </div>

    <button class="btn" id="btn" onclick="go()" disabled>选择图片后开始</button>
    <div class="status" id="st"><span id="sttext"></span></div>
  </div>

  <div class="features">
    <div class="feat"><div class="feat-icon">⚡</div><div class="feat-title">精确压缩</div><div class="feat-desc">二分法算法，紧贴目标大小，不会超限</div></div>
    <div class="feat"><div class="feat-icon">🔒</div><div class="feat-title">不留存文件</div><div class="feat-desc">处理完即删除，图片不保存在服务器</div></div>
    <div class="feat"><div class="feat-icon">🆓</div><div class="feat-title">完全免费</div><div class="feat-desc">无需注册，无次数限制，直接用</div></div>
  </div>
</div>

<script>
let f=null;
const dz=document.getElementById('dz'),fi=document.getElementById('fi'),
      prev=document.getElementById('prev'),pimg=document.getElementById('pimg'),
      pname=document.getElementById('pname'),psize=document.getElementById('psize'),
      btn=document.getElementById('btn'),st=document.getElementById('st'),
      sttext=document.getElementById('sttext');

dz.onclick=()=>fi.click();
dz.ondragover=e=>{e.preventDefault();dz.classList.add('over')};
dz.ondragleave=()=>dz.classList.remove('over');
dz.ondrop=e=>{e.preventDefault();dz.classList.remove('over');if(e.dataTransfer.files[0])load(e.dataTransfer.files[0])};
fi.onchange=e=>{if(e.target.files[0])load(e.target.files[0])};

function load(file){
  if(!file.type.startsWith('image/')){show('err','请选择图片文件');return}
  f=file;
  const r=new FileReader();
  r.onload=e=>{pimg.src=e.target.result;pname.textContent=file.name;psize.textContent='原始大小：'+(file.size/1024).toFixed(1)+' KB';prev.classList.add('show');dz.style.display='none'};
  r.readAsDataURL(file);
  btn.disabled=false;btn.textContent='开始压缩 →';st.className='status';
}

function set(kb){
  document.getElementById('tgt').value=kb;
  document.querySelectorAll('.preset').forEach(b=>b.classList.toggle('on',parseInt(b.textContent)===kb||(kb===1024&&b.textContent.includes('1 MB'))||(kb===2048&&b.textContent.includes('2 MB'))));
}

async function go(){
  if(!f)return;
  const kb=parseFloat(document.getElementById('tgt').value);
  if(!kb||kb<=0){show('err','请输入有效的目标大小');return}
  btn.disabled=true;btn.textContent='压缩中...';
  st.className='status show loading';
  st.innerHTML='<div class="spinner"></div><span>正在处理，请稍候...</span>';

  const fd=new FormData();fd.append('image',f);fd.append('target_kb',kb.toString());
  try{
    const resp=await fetch('/compress',{method:'POST',body:fd});
    if(!resp.ok){show('err','压缩失败：'+await resp.text());return}
    const blob=await resp.blob();
    const rKB=(blob.size/1024).toFixed(1),oKB=(f.size/1024).toFixed(1);
    const saved=(((f.size-blob.size)/f.size)*100).toFixed(0);
    const url=URL.createObjectURL(blob),a=document.createElement('a');
    const cd=resp.headers.get('Content-Disposition')||'',m=cd.match(/filename="(.+?)"/);
    a.download=m?m[1]:'compressed.jpg';a.href=url;a.click();URL.revokeObjectURL(url);
    show('ok',oKB+' KB → '+rKB+' KB · 减少 '+saved+'% · 已自动下载');
    psize.textContent='原始：'+oKB+' KB → 压缩后：'+rKB+' KB';
  }catch(e){show('err','网络错误，请重试')}
  finally{btn.disabled=false;btn.textContent='再次压缩 →'}
}

function show(type,msg){
  st.className='status show '+type;
  st.innerHTML='<div class="sdot"></div><span>'+msg+'</span>';
}
</script>

<style>
.faq{margin-top:48px}
.faq-title{font-family:'Syne',sans-serif;font-size:22px;font-weight:800;margin-bottom:24px;letter-spacing:-.01em}
.faq-item{border-bottom:1px solid var(--border);padding:20px 0;cursor:pointer}
.faq-item:last-child{border-bottom:none}
.faq-q{font-family:'Syne',sans-serif;font-size:15px;font-weight:700;display:flex;justify-content:space-between;align-items:center;gap:16px}
.faq-arrow{color:var(--muted);font-size:18px;flex-shrink:0;transition:transform .2s}
.faq-item.open .faq-arrow{transform:rotate(45deg)}
.faq-a{font-size:14px;color:var(--muted);line-height:1.75;max-height:0;overflow:hidden;transition:max-height .3s ease,padding .3s}
.faq-item.open .faq-a{max-height:300px;padding-top:12px}
</style>

<div class="faq">
  <div class="faq-title">常见问题</div>

  <div class="faq-item" onclick="this.classList.toggle('open')">
    <div class="faq-q">如何把图片压缩到200KB以内？<span class="faq-arrow">+</span></div>
    <div class="faq-a">上传图片后，在目标大小输入框填写"200"，点击"开始压缩"，工具会自动将图片压缩到200KB以内并下载。适合政府表单、招聘网站等对图片大小有严格限制的场景。</div>
  </div>

  <div class="faq-item" onclick="this.classList.toggle('open')">
    <div class="faq-q">图片压缩后会不会很模糊？<span class="faq-arrow">+</span></div>
    <div class="faq-a">我们使用二分法算法，在满足目标大小的前提下尽量保留最高画质。目标大小设置越接近原图大小，画质损失越小。建议目标大小不要设置得过小，否则任何压缩工具都会损失画质。</div>
  </div>

  <div class="faq-item" onclick="this.classList.toggle('open')">
    <div class="faq-q">支持压缩到1MB、500KB、100KB吗？<span class="faq-arrow">+</span></div>
    <div class="faq-a">支持任意目标大小，可以直接点击预设按钮（100KB、200KB、500KB、1MB、2MB），也可以手动输入任意数值。无论是压缩到1MB还是50KB都可以处理。</div>
  </div>

  <div class="faq-item" onclick="this.classList.toggle('open')">
    <div class="faq-q">图片会上传到服务器保存吗？<span class="faq-arrow">+</span></div>
    <div class="faq-a">不会。图片上传后仅用于压缩处理，处理完成后立即删除，不会保存在服务器上。你的图片数据完全安全。</div>
  </div>

  <div class="faq-item" onclick="this.classList.toggle('open')">
    <div class="faq-q">支持哪些图片格式？PNG能压缩吗？<span class="faq-arrow">+</span></div>
    <div class="faq-a">支持 JPG、PNG、WebP 格式。PNG 图片会自动转换为 JPG 格式进行压缩，压缩效果更好，文件体积更小。最大支持20MB的图片文件。</div>
  </div>

  <div class="faq-item" onclick="this.classList.toggle('open')">
    <div class="faq-q">微信发图片太模糊，怎么压缩后还能保持清晰？<span class="faq-arrow">+</span></div>
    <div class="faq-a">微信会对超过5MB的图片进行二次压缩。建议将图片压缩到4MB以内再发送，可以有效避免微信的自动压缩，保持图片清晰度。在目标大小输入"4096"（即4MB）即可。</div>
  </div>

  <div class="faq-item" onclick="this.classList.toggle('open')">
    <div class="faq-q">这个工具免费吗？有使用次数限制吗？<span class="faq-arrow">+</span></div>
    <div class="faq-a">完全免费，无需注册，无使用次数限制，直接使用即可。</div>
  </div>
</div>
</body>
</html>`
