package download

import (
    "fmt"
    "path/filepath"
    "strings"
    "sync"

    "github.com/GopeedLab/gopeed/pkg/base"
)

// DownloadFileProvider is implemented by the Android MediaStore bridge.
// All file data stays in the public Download directory. Files are attributed
// to the application and require no media-reading or broad storage permission.
type DownloadFileProvider interface {
    Directory() string
    Prepare(fileName string) string
    Publish(fullPath string) bool
    Remove(fullPath string) bool
}

var privateStorage struct {
    sync.RWMutex
    provider DownloadFileProvider
}

func RegisterPrivateAndroidStorage(provider DownloadFileProvider) {
    privateStorage.Lock()
    privateStorage.provider = provider
    privateStorage.Unlock()
}

func privateAndroidDirectory() string {
    if !base.PrivateAndroid { return "" }
    privateStorage.RLock()
    p := privateStorage.provider
    privateStorage.RUnlock()
    if p == nil { return "" }
    dir := filepath.Clean(p.Directory())
    if dir == "." || !filepath.IsAbs(dir) || filepath.Base(dir) != "Download" { return "" }
    return dir
}

func privateAndroidPrepare(name string) (string, error) {
    if !base.PrivateAndroid { return "", nil }
    dir := privateAndroidDirectory()
    if dir == "" { return "", fmt.Errorf("Android MediaStore Download directory not initialized") }
    if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
        return "", fmt.Errorf("invalid download filename")
    }
    privateStorage.RLock()
    p := privateStorage.provider
    privateStorage.RUnlock()
    if p == nil { return "", fmt.Errorf("MediaStore callback unavailable") }
    actual := filepath.Clean(p.Prepare(name))
    if filepath.Dir(actual) != dir || filepath.Base(actual) == "." {
        return "", fmt.Errorf("MediaStore refused Download location")
    }
    return actual, nil
}

func privateAndroidPublish(path string) error {
    if !base.PrivateAndroid { return nil }
    dir := privateAndroidDirectory()
    if dir == "" || filepath.Dir(filepath.Clean(path)) != dir {
        return fmt.Errorf("Android download outside Download directory")
    }
    privateStorage.RLock()
    p := privateStorage.provider
    privateStorage.RUnlock()
    if p == nil || !p.Publish(path) { return fmt.Errorf("unable to publish MediaStore Download") }
    return nil
}

func privateAndroidRemove(path string) bool {
    if !base.PrivateAndroid { return false }
    dir := privateAndroidDirectory()
    if dir == "" || filepath.Dir(filepath.Clean(path)) != dir { return false }
    privateStorage.RLock()
    p := privateStorage.provider
    privateStorage.RUnlock()
    return p != nil && p.Remove(path)
}
