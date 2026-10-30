# Gopeed Gizli İndirici (Android / Java)

Bu klasör, Gopeed reposundaki normal Flutter/Go/BitTorrent istemcisinden **tamamen bağımsız**, güvenlik için küçültülmüş Android uygulamasıdır. GitHub Releases'a yüklenen `Gopeed-Gizli-Indirici.apk` **yalnızca** buradaki Java dosyalarından derlenir. Eski Gopeed motoru APK'ye alınmaz.

## Çalışma ve ağ sınırları

- Uygulama açıldığında hiçbir sunucuya bağlanmaz. Kullanıcı **Download klasörüne indir** düğmesine bastığında **tek bir HTTPS GET** işlemi başlatılır.
- HTTPS dışı adres, 3xx yönlendirme ve otomatik yeniden deneme reddedilir. Doğrudan HTTP proxy kullanımı engellenmiştir. Harici DNS/HTTPS trafiği yalnızca istenen dosyaya erişimin doğal parçasıdır.
- Dosya yalnızca `MediaStore.Downloads` ile telefonun birincil depolamasındaki **Download** klasörüne yazılır. Android 10+ için dosya/fotoğraf okuma ve geniş depolama izni gerekmez.
- Uygulama telefon dosyalarını **okumaz**, listelemez veya göndermez. Başarısız indirmelerde oluşturulan kısmi dosyayı kaldırır.
- Analitik, kimlik/Android ID okuma, otomatik güncelleme, eklenti, WebView, API sunucusu, webhook, torrent, magnet, JavaScript ve üçüncü taraf SDK yoktur.
- Başlatılan transfer bildirimli **foreground service** ile uygulama ekranı kapalıyken devam edebilir. Açılışta/telefon yeniden başlayınca otomatik çalışma yoktur.
- Manifest izinleri yalnızca `INTERNET`, `FOREGROUND_SERVICE`, `FOREGROUND_SERVICE_DATA_SYNC`, `POST_NOTIFICATIONS`. Konum, fotoğraf, rehber, kamera, mikrofon, genel dosya okuma veya paket yükleme izni yoktur.

## Mutlak gizlilik hakkında

**Dosya indirmek için seçtiğiniz dosya sunucusuna bağlantı zorunludur.** Sunucu, bağlantı IP adresini, istenen URL'yi ve normal HTTPS protokol bilgilerinin bir kısmını görebilir. DNS sorguları Android'in/ağın DNS sağlayıcısından geçebilir. Telefon işletim sistemi, operatör veya diğer uygulamaların oluşturduğu ağ trafiğini bu APK engelleyemez. Uygulamanın **kendisi**, seçilen indirme adresi dışında bir sunucuya istek gönderecek kod içermez.

## Bilinçli kısıtlamalar

- HTTP, torrent/magnet, redirect kullanan indirme bağlantıları, cookie/login, proxy, özel header, çoklu parçaya bölünmüş indirme ve indirmeyi devam ettirme desteklenmez.
- Her seferinde bir dosya indirilir. İptal edilen dosya silinir.
- APK GitHub Actions tarafından, henüz kalıcı imzalama anahtarı yapılandırılmadığı için **geçici debug anahtarıyla** imzalanır. Gelecekte aynı paket üzerine yükleme yapabilmek için özel ve kalıcı bir signing key gerekir; aksi takdirde eski sürümü kaldırmak gerekebilir.

## Derleme

```bash
sdkmanager "platforms;android-36" "build-tools;36.0.0"
gradle -p private-android assembleRelease
```

Derlenen APK: `private-android/app/build/outputs/apk/release/app-release.apk`.

Release otomatik olarak GitHub Actions tarafından oluşturulur:
https://github.com/mrcan321/gopeed/releases/tag/privacy-only-latest
