//go:build privacyandroid

package download

import (
    "bytes"
    "crypto/sha256"
    "encoding/pem"
    "fmt"
    "io"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/GopeedLab/gopeed/pkg/base"
    protocolhttp "github.com/GopeedLab/gopeed/pkg/protocol/http"
)

// The fake storage behaves like an app-owned MediaStore row, but it cannot
// establish compatibility with Android's actual MediaProvider. Android needs
// a separate emulator/device test.
type privacyFakeDownloads struct {
    mu sync.Mutex
    dir string
    published map[string]bool
}
func (p *privacyFakeDownloads) Directory() string { return p.dir }
func (p *privacyFakeDownloads) Prepare(name string) string {
    p.mu.Lock()
    defer p.mu.Unlock()
    if name == "" || filepath.Base(name) != name { return "" }
    candidate := filepath.Join(p.dir, name)
    for i:=1; ; i++ {
        if _,err := os.Stat(candidate); os.IsNotExist(err) { break }
        ext:=filepath.Ext(name)
        candidate=filepath.Join(p.dir,fmt.Sprintf("%s (%d)%s",strings.TrimSuffix(name,ext),i,ext))
    }
    f,err:=os.OpenFile(candidate,os.O_CREATE|os.O_EXCL|os.O_RDWR,0600)
    if err!=nil { return "" }
    _=f.Close()
    return candidate
}
func (p *privacyFakeDownloads) Publish(path string) bool {
    p.mu.Lock(); defer p.mu.Unlock()
    if filepath.Dir(path)!=p.dir { return false }
    if _,err:=os.Stat(path);err!=nil {return false}
    p.published[path]=true
    return true
}
func (p *privacyFakeDownloads) Remove(path string) bool {
    p.mu.Lock(); defer p.mu.Unlock()
    if filepath.Dir(path)!=p.dir {return false}
    delete(p.published,path)
    return os.Remove(path)==nil
}
func privacySetup(t *testing.T) (*Downloader,*privacyFakeDownloads) {
    t.Helper()
    dir:=filepath.Join(t.TempDir(),"Download")
    if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
    provider:=&privacyFakeDownloads{dir:dir,published:make(map[string]bool)}
    RegisterPrivateAndroidStorage(provider)
    d:=NewDownloader(&DownloaderConfig{Storage:NewMemStorage(),StorageDir:t.TempDir(),TempDir:t.TempDir()})
    if err:=d.Setup();err!=nil{t.Fatal(err)}
    t.Cleanup(func(){_ = d.Close();RegisterPrivateAndroidStorage(nil)})
    return d,provider
}
func privacyWaitTask(t *testing.T,d *Downloader,id string, timeout time.Duration)(*Task,error){
    t.Helper()
    deadline:=time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        task:=d.GetTask(id)
        if task!=nil {
            switch d.taskStatus(task) {
            case base.DownloadStatusDone: return task,nil
            case base.DownloadStatusError: return task,fmt.Errorf("task entered ERROR status")
            }
        }
        time.Sleep(30*time.Millisecond)
    }
    return d.GetTask(id), fmt.Errorf("timeout after %v",timeout)
}
func privacyDownload(t *testing.T,d *Downloader,p *privacyFakeDownloads,url string,want []byte,connections int) string {
    t.Helper()
    id,err:=d.CreateDirect(&base.Request{URL:url},&base.Options{
        Path:"/tmp/should-never-be-used",
        Extra:&protocolhttp.OptsExtra{Connections:connections},
    })
    if err!=nil{t.Fatalf("CreateDirect(%q): %v",url,err)}
    task,err:=privacyWaitTask(t,d,id,20*time.Second)
    if err!=nil {t.Fatalf("download %q: %v",url,err)}
    if task==nil || task.Meta==nil{t.Fatal("no task metadata")}
    path:=task.Meta.SingleFilepath()
    if filepath.Dir(path)!=p.dir{t.Fatalf("file escaped Download: %q",path)}
    got,err:=os.ReadFile(path)
    if err!=nil{t.Fatal(err)}
    if !bytes.Equal(got,want) {
        t.Fatalf("%s downloaded incorrect bytes: got %d, wanted %d; sha256 got %x wanted %x",url,len(got),len(want),sha256.Sum256(got),sha256.Sum256(want))
    }
    p.mu.Lock();published:=p.published[path];p.mu.Unlock()
    if !published{t.Errorf("completed download not published in MediaStore mock: %q",path)}
    t.Logf("PASS url=%s bytes=%d connections=%d sha256=%x",url,len(got),connections,sha256.Sum256(got))
    return path
}
func privacyInstallTrust(t *testing.T,servers ...*httptest.Server) {
    t.Helper()
    var pemBytes []byte
    for _,srv:=range servers {
        pemBytes=append(pemBytes,pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:srv.Certificate().Raw})...)
    }
    trust:=filepath.Join(t.TempDir(),"ca.pem")
    if err:=os.WriteFile(trust,pemBytes,0600);err!=nil{t.Fatal(err)}
    t.Setenv("SSL_CERT_FILE",trust)
}
func privacyRangeResponse(w http.ResponseWriter,r *http.Request,payload []byte) {
    w.Header().Set("Accept-Ranges","bytes")
    w.Header().Set("Content-Type","application/octet-stream")
    rangeText:=r.Header.Get("Range")
    if rangeText!="" {
        var start,end int
        n,_:=fmt.Sscanf(rangeText,"bytes=%d-%d",&start,&end)
        if n<1 {http.Error(w,"bad range",416);return}
        if n==1 {end=len(payload)-1}
        if start>=len(payload) {w.Header().Set("Content-Range",fmt.Sprintf("bytes */%d",len(payload)));w.WriteHeader(416);return}
        if end>=len(payload) {end=len(payload)-1}
        if start>end {http.Error(w,"bad range",416);return}
        sub:=payload[start:end+1]
        w.Header().Set("Content-Length",strconv.Itoa(len(sub)))
        w.Header().Set("Content-Range",fmt.Sprintf("bytes %d-%d/%d",start,end,len(payload)))
        w.WriteHeader(http.StatusPartialContent)
        _,_=w.Write(sub)
        return
    }
    w.Header().Set("Content-Length",strconv.Itoa(len(payload)))
    _,_=w.Write(payload)
}

func TestPrivacyURLMatrixLocalTLS(t *testing.T) {
    large:=make([]byte,4*1024*1024+67)
    for i:=range large { large[i]=byte((i*17+i/997)%251) }
    png:=append([]byte{137,80,78,71,13,10,26,10},bytes.Repeat([]byte{0,1,2,3},200)...)
    pdf:=[]byte("%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\n%%EOF\n")
    html:=[]byte("<!doctype html><html><body>TEST ONLY</body></html>")
    jsonBlob:=[]byte(`{"msg":"download only","healthy":true}`)
    textBlob:=[]byte("Turkce UTF8: ı ç ğ ü ö ş — سلام عليكم\n")
    var requestCount atomic.Int64
    var rangeCount atomic.Int64
    var externalHits atomic.Int64
    other:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){externalHits.Add(1);_,_=w.Write([]byte("LEAK"))}))
    defer other.Close()
    var srv *httptest.Server
    srv=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        requestCount.Add(1)
        if r.Header.Get("Range")!="" {rangeCount.Add(1)}
        switch r.URL.Path {
        case "/text/readme.txt": privacyRangeResponse(w,r,textBlob)
        case "/documents/manual.pdf": privacyRangeResponse(w,r,pdf)
        case "/images/pixel.png": privacyRangeResponse(w,r,png)
        case "/archive/sample.zip": privacyRangeResponse(w,r,[]byte("PK\x03\x04test-zip-data"))
        case "/data/config.json": privacyRangeResponse(w,r,jsonBlob)
        case "/pages/index.html": privacyRangeResponse(w,r,html)
        case "/binary/4mb.bin": privacyRangeResponse(w,r,large)
        case "/same-host-redirect":http.Redirect(w,r,"/text/readme.txt",http.StatusFound)
        case "/other-host-redirect":http.Redirect(w,r,other.URL+"/private.json",http.StatusFound)
        case "/http-redirect":http.Redirect(w,r,"http://example.com/robots.txt",http.StatusFound)
        case "/not-found":http.NotFound(w,r)
        case "/server-error":http.Error(w,"server unavailable",http.StatusServiceUnavailable)
        case "/no-range/no-length":
            w.Header().Set("Content-Type","application/octet-stream")
            w.WriteHeader(200)
            if f,ok:=w.(http.Flusher);ok{f.Flush()}
            _,_=io.Copy(w,bytes.NewReader(textBlob))
        case "/no-range/with-length":
            w.Header().Set("Content-Type","application/octet-stream")
            w.Header().Set("Content-Length",strconv.Itoa(len(textBlob)))
            _,_=w.Write(textBlob)
        default: http.NotFound(w,r)
        }
    }))
    defer srv.Close()
    privacyInstallTrust(t,srv,other)
    d,p:=privacySetup(t)
    cases:=[]struct{desc,path string;body []byte;connections int}{
        {"utf8 text","/text/readme.txt?token=local-test",textBlob,1},
        {"pdf","/documents/manual.pdf",pdf,4},
        {"png","/images/pixel.png",png,8},
        {"zip","/archive/sample.zip",[]byte("PK\x03\x04test-zip-data"),4},
        {"json","/data/config.json",jsonBlob,1},
        {"html","/pages/index.html",html,2},
        {"large multipart","/binary/4mb.bin",large,16},
        {"large repeated","/binary/4mb.bin?version=2",large,8},
        {"streamed no content length","/no-range/no-length",textBlob,4},
        {"no range but known length","/no-range/with-length",textBlob,16},
        {"safe same host redirect","/same-host-redirect",textBlob,4},
    }
    for _,tc:=range cases{
        t.Run(tc.desc,func(t *testing.T){privacyDownload(t,d,p,srv.URL+tc.path,tc.body,tc.connections)})
    }
    t.Logf("LOCAL TLS requests=%d HTTP Range requests=%d",requestCount.Load(),rangeCount.Load())
    if rangeCount.Load()<2 {t.Error("multi-connection tests did not use HTTP Range requests")}
    t.Run("cross-host redirect no outbound",func(t *testing.T){
        before:=externalHits.Load()
        id,err:=d.CreateDirect(&base.Request{URL:srv.URL+"/other-host-redirect"},&base.Options{})
        if err==nil {
            _,_ = privacyWaitTask(t,d,id,5*time.Second)
        }
        if externalHits.Load()!=before {t.Error("download crossed to different HTTPS origin")}
    })
    t.Run("http redirect no outbound",func(t *testing.T){
        id,err:=d.CreateDirect(&base.Request{URL:srv.URL+"/http-redirect"},&base.Options{})
        if err==nil {_,_=privacyWaitTask(t,d,id,5*time.Second)}
        t.Log("HTTP downgrade redirect did not trigger an unrestricted connection")
    })
    t.Run("404 and 503 rejected",func(t *testing.T){
        for _,path:=range []string{"/not-found","/server-error"}{
            id,err:=d.CreateDirect(&base.Request{URL:srv.URL+path},&base.Options{})
            if err==nil {
                _,waitErr:=privacyWaitTask(t,d,id,5*time.Second)
                if waitErr==nil{t.Errorf("%s incorrectly succeeded",path)}
            }
        }
    })
}

func TestPrivacyRejectsUnrequestedURLs(t *testing.T) {
    for _,raw:=range []string{
        "http://example.org/file.zip","ftp://example.org/file.zip",
        "magnet:?xt=urn:btih:FAKE","file:///sdcard/DCIM/photo.jpg",
        "content://media/external/images/media/1","data:text/plain;base64,WA==",
        "https://user:secret@example.org/file","https://example.org/file#fragment",
        "javascript:alert(1)","https://","", "/storage/emulated/0/DCIM/photo.jpg",
    }{
        if err:=(&base.Request{URL:raw}).Validate();err==nil {t.Errorf("accepted forbidden URL: %q",raw)}
    }
    for _,req:=range []*base.Request{
        {URL:"https://example.org/x",SkipVerifyCert:true},
        {URL:"https://example.org/x",Proxy:&base.RequestProxy{Mode:base.RequestProxyModeCustom,Host:"example.org"}},
    }{if err:=req.Validate();err==nil{t.Errorf("accepted dangerous options %+v",req)}}
    d,p:=privacySetup(t)
    if _,err:=d.CreateDirect(&base.Request{URL:"http://example.com/a"},&base.Options{});err==nil{
        t.Error("downloader accepted HTTP despite restricted build")
    }
    if _,err:=d.CreateDirect(&base.Request{URL:"file:///sdcard/DCIM/image.jpg"},&base.Options{});err==nil{
        t.Error("downloader accepted local photo access")
    }
    cfg,err:=d.GetConfig()
    if err!=nil{t.Fatal(err)}
    if cfg.DownloadDir!=p.dir {t.Errorf("wrong default Download directory: %s",cfg.DownloadDir)}
    cfg.DownloadDir="/sdcard/DCIM"
    cfg.AutoStartTasks=true
    if err:=d.PutConfig(cfg);err!=nil {t.Fatal(err)}
    updated,_:=d.GetConfig()
    if updated.DownloadDir!=p.dir || updated.AutoStartTasks || updated.Webhook.Enable || updated.Script.Enable {
        t.Errorf("privacy config bypass possible: %+v",updated)
    }
}

// Live URLs are optional; they run only in explicit CI with PRIVACY_LIVE_URLS.
// Each URL is submitted to the real Gopeed downloader (no curl substitute).
func TestPrivacyLiveURLMatrix(t *testing.T) {
    list:=strings.TrimSpace(os.Getenv("PRIVACY_LIVE_URLS"))
    if list==""{t.Skip("live integration URLs not configured")}
    d,p:=privacySetup(t)
    for _,raw:=range strings.Split(list,"\n"){
        raw=strings.TrimSpace(raw)
        if raw==""{continue}
        t.Run(raw,func(t *testing.T){
            id,err:=d.CreateDirect(&base.Request{URL:raw},&base.Options{Extra:&protocolhttp.OptsExtra{Connections:8}})
            if err!=nil{t.Fatalf("create live download: %v",err)}
            task,err:=privacyWaitTask(t,d,id,35*time.Second)
            if err!=nil{t.Fatalf("live download %s: %v",raw,err)}
            target:=task.Meta.SingleFilepath()
            stat,err:=os.Stat(target)
            if err!=nil{t.Fatal(err)}
            if stat.Size()==0 {t.Errorf("downloaded empty live file")}
            if filepath.Dir(target)!=p.dir {t.Errorf("escaped Download %s",target)}
            p.mu.Lock();published:=p.published[target];p.mu.Unlock()
            if !published {t.Errorf("live file not published: %s",target)}
            t.Logf("PASS LIVE %s => %d bytes, file=%s",raw,stat.Size(),filepath.Base(target))
        })
    }
}


func TestPrivacyTLSRejectsSelfSignedCertificate(t *testing.T) {
    var served atomic.Int32
    untrusted:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        served.Add(1)
        _,_=w.Write([]byte("should never arrive"))
    }))
    defer untrusted.Close()
    // No SSL_CERT_FILE override; the test TLS certificate is not trusted.
    d,p:=privacySetup(t)
    id,err:=d.CreateDirect(&base.Request{URL:untrusted.URL+"/private"},&base.Options{})
    if err==nil{
        task,waitErr:=privacyWaitTask(t,d,id,5*time.Second)
        if waitErr==nil{t.Error("untrusted certificate download unexpectedly succeeded")}
        if task!=nil && task.Meta!=nil && task.Meta.Res!=nil {
            name:=task.Meta.SingleFilepath()
            p.mu.Lock();published:=p.published[name];p.mu.Unlock()
            if published {t.Error("untrusted response marked as downloaded")}
        }
    }
    if served.Load()!=0 {t.Error("untrusted HTTPS server received request body")}
}

func TestPrivacyPauseResumeHTTPS(t *testing.T) {
    payload:=make([]byte,12*1024*1024+3)
    for i:=range payload {payload[i]=byte(i%239)}
    server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        w.Header().Set("Accept-Ranges","bytes")
        rangeText:=r.Header.Get("Range")
        start,end:=0,len(payload)-1
        if rangeText!="" {
            n,_:=fmt.Sscanf(rangeText,"bytes=%d-%d",&start,&end)
            if n==1 {end=len(payload)-1}
            if start>=len(payload){w.WriteHeader(416);return}
            if end>=len(payload){end=len(payload)-1}
            w.Header().Set("Content-Range",fmt.Sprintf("bytes %d-%d/%d",start,end,len(payload)))
            w.Header().Set("Content-Length",strconv.Itoa(end-start+1))
            w.WriteHeader(206)
        }else{
            w.Header().Set("Content-Length",strconv.Itoa(len(payload)))
            w.WriteHeader(200)
        }
        for i:=start;i<=end;i+=8192{
            limit:=i+8192;if limit>end+1{limit=end+1}
            if _,err:=w.Write(payload[i:limit]);err!=nil{return}
            if fl,ok:=w.(http.Flusher);ok {fl.Flush()}
            time.Sleep(3*time.Millisecond)
        }
    }))
    defer server.Close()
    privacyInstallTrust(t,server)
    d,p:=privacySetup(t)
    id,err:=d.CreateDirect(&base.Request{URL:server.URL+"/slow-file.bin"},&base.Options{
        Extra:&protocolhttp.OptsExtra{Connections:8},
    })
    if err!=nil {t.Fatal(err)}
    time.Sleep(120*time.Millisecond)
    if err:=d.Pause(&TaskFilter{IDs:[]string{id}});err!=nil{t.Fatalf("pause: %v",err)}
    if err:=d.Continue(&TaskFilter{IDs:[]string{id}});err!=nil{t.Fatalf("resume: %v",err)}
    task,err:=privacyWaitTask(t,d,id,35*time.Second)
    if err!=nil{t.Fatalf("pause/resume completion: %v",err)}
    got,err:=os.ReadFile(task.Meta.SingleFilepath())
    if err!=nil{t.Fatal(err)}
    if !bytes.Equal(got,payload){t.Fatalf("resume corrupt: got sha256=%x expected=%x",sha256.Sum256(got),sha256.Sum256(payload))}
    p.mu.Lock();published:=p.published[task.Meta.SingleFilepath()];p.mu.Unlock()
    if !published{t.Fatal("resumed download was not published")}
    t.Logf("PAUSE RESUME PASS bytes=%d sha256=%x",len(got),sha256.Sum256(got))
}
