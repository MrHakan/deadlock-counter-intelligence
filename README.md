# Deadlock Counter Intelligence

Deadlock Mod Manager için deneysel bir Panorama HUD başlangıcı. Skor tablosu veya mağaza açıldığında en fazla üç karşı item, soul fiyatı ve gerekçesi gösterir. İstemcinin mağazadaki **görünen son alımları** rakip kadrosuyla eşleşirse öneriler 0,5 saniyelik aralıklarla güncellenir. Bunun dışında desteklenen rakip kahramanlardan öneri üretir.

Öneri motoru, örnek Panorama panelleri ve arşiv oluşturma yerelde test edilir. Bu sürümün Deadlock veya Mod Manager içinde kurulumu ve çalışması henüz test edilmedi. Rakibin tam envanteri ve satışları takip edilmez. Mod maç sırasında API çağrısı, oyun belleği erişimi veya giriş otomasyonu yapmaz.

## Deneysel VPK oluşturma

Gerekenler Python 3.9+, Steam üzerinden kurulu Deadlock ve [Source2Viewer-CLI](https://github.com/ValveResourceFormat/ValveResourceFormat/releases). Kurulu oyundan alınan `hud.vxml_c` derleme girdisidir.

```powershell
py -3 tools/build.py --deadlock-dir "C:\Program Files (x86)\Steam\steamapps\common\Deadlock" --vrf "C:\Tools\Source2Viewer-CLI.exe"
```

Zaten açılmış orijinal HUD XML'i varsa `--hud-xml path/to/hud.xml` kullan. Oyun içi metinleri İngilizce üretmek için `--language english` ekle. Paket `dist/deadlock-counter-intelligence-experimental.zip` yolunda oluşur ve tek bir `pak01_dir.vpk` içerir.

Deadlock kapalıyken paketi Mod Manager'da **Mods Library → Add Local Mod** ile içe aktar, etkinleştir ve oyunu Mod Manager üzerinden aç. Kurulum menüsünün hedef sürümü v2.3.0. Deneysel HUD'un kaldırılması Mod Manager'ın devre dışı bırakma veya kaldırma işlemiyle yapılır. Bu paket `panorama/layout/hud.vxml_c` dosyasını değiştirdiği için aynı kaynağı değiştiren HUD modlarıyla çakışabilir.

## Katalog ve testler

Birlikte gelen katalog istemci sürümü **6861** için 173 satın alınabilir item ve 44 kahraman kaydı içerir. İngilizce ve Türkçe adlar aynı oyun kimlikleri üzerinden eşleştirilir. Yeni bir sürüm için katalog numarasını ve HUD girdisini birlikte güncelle.

```text
python tools/update_catalog.py 6861
node --test test/intelligence.test.js
python -m unittest discover -s test -v
```

İlk kurallar iyileşme, mermi hasarı, spirit hasarı ve havadaki hedefleri kapsar. Okunamayan veya birden çok itemle eşleşen etiketler atlanır. Mağaza kapandığında mağaza gözlemleri bırakılır. Gözlem, satın alım satırının görünür olmasına dayanır ve sahiplik göstergesi olarak kullanılmaz.

İlk oyun içi doğrulamada temiz bir Mod Manager profili ve özel lobi kullanılabilir. Tek bir rakip görünür örnek item aldığında öneri değişimini, müttefik alımının atlandığını ve mod kapatıldığında vanilla HUD'un geri geldiğini kontrol et.

## Kaynaklar

- [Deadlock Mod Manager yerel mod kılavuzu](https://docs.deadlockmods.app/using-mod-manager/library#add-local-mods) ve [Live bridge](https://github.com/deadlock-mod-manager/deadlock-mod-manager/blob/v2.3.0/apps/desktop/src-tauri/src/live_bridge/dmm_live_bridge.js)
- [Deadlock API](https://api.deadlock-api.com/docs) ve [GameTracking-Deadlock HUD düzeni](https://github.com/SteamTracking/GameTracking-Deadlock/blob/master/game/citadel/pak01_dir/panorama/layout/hud.xml)
- [Threat HUD mağaza düzeni](https://github.com/antonborndev/deadlock-threat-hud/blob/9fc4cbfd7290c834eb81490ecc004c578ae4fbba/threathud/panorama/layout/citadel_hud_hero_shop.xml) ve [BiggerIdleMap derleme örneği](https://github.com/regulad/BiggerIdleMap)
- `third_party/panorama.py`, BiggerIdleMap'teki [MIT lisanslı Panorama derleyicisinden](https://github.com/regulad/BiggerIdleMap/blob/d54483205cbc01a3f496d536e338f58b919c2caa/panorama.py) alınmıştır. Orijinal telif ve lisans bildirimi dosyada yer alır.
