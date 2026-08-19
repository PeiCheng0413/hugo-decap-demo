# 山戶 SANTO｜Hugo + Decap CMS 功能示範站

一個產品展示型的靜態網站，用來示範「**Hugo 產生靜態站 + Decap CMS 當網頁後台 + GitHub Actions 自動上線**」這套組合怎麼運作。

> **品牌、商品、價格、規格全部是虛構的示範資料**，照片來自 [picsum.photos](https://picsum.photos) 隨機圖庫，與任何真實產品無關。

## 這個示範站示範了什麼

| 功能 | 在哪看 |
|---|---|
| 產品集合的增刪改（含圖片上傳） | `/admin/` → 產品 |
| 自訂欄位：價格、型號、分類下拉、規格表、相簿 | `/admin/` → 產品 → 任一產品 |
| 首頁區塊文案與背景圖可改 | `/admin/` → 頁面 → 首頁區塊一／二 |
| 導覽選單可從後台增減 | `/admin/` → 站台設定 → 導覽選單 |
| SEO／分享縮圖設定可從後台改 | `/admin/` → 站台設定 → 站台參數 |
| 頁尾電話信箱（Hugo data 檔）可從後台改 | `/admin/` → 站台設定 → 聯絡資訊 |
| 存檔即 commit 回 GitHub，Actions 自動重建上線 | repo 的 Actions 分頁 |

## 技術組成

- **Hugo extended 0.165**（本機實測版本，最低需求 0.128）— 零 node 依賴，主題的 SCSS 由 Hugo 自己編譯。
- **主題：[Hugo Hero](https://github.com/zerostaticthemes/hugo-hero-theme)**（Zerostatic，MIT 授權），直接 vendored 在 `themes/` 底下沒有用 submodule，CI 不會漏抓。
- **Decap CMS 3.x**，從 CDN 載入，沒有任何前端建置流程。
- **GitHub Pages + Actions** 部署；OAuth 中繼站是一支 Go 程式，跑在 fly.io。

## 本機開發

```bash
hugo server        # → http://localhost:1414/hugo-decap-demo/
```

想在本機測試後台（不需要登入 GitHub、直接寫本機檔案）：

```bash
npx decap-server   # 另開一個終端機視窗，跑在 8081
hugo server
# 然後開 http://localhost:1414/hugo-decap-demo/admin/
```

`static/admin/config.yml` 裡的 `local_backend: true` 就是打開這個模式用的。

## 上線狀態

- 網站：<https://peicheng0413.github.io/hugo-decap-demo/> ← 已上線，push 到 main 就自動重建
- 後台：<https://peicheng0413.github.io/hugo-decap-demo/admin/> ← 要能登入，還差下面兩步

### 還沒完成的兩步（讓 /admin 可以用 GitHub 登入）

**1. 建 GitHub OAuth App**

Settings → Developer settings → OAuth Apps → New OAuth App，填：

| 欄位 | 值 |
|---|---|
| Application name | 隨便，例如 `Decap CMS - hugo-decap-demo` |
| Homepage URL | `https://peicheng0413.github.io/hugo-decap-demo/` |
| Authorization callback URL | `https://peicheng-decap-oauth.fly.dev/callback` |

建完按 **Generate a new client secret**，client ID 與 secret 留著下一步用。

**2. 部署 OAuth 中繼站到 fly.io**

Decap 用 GitHub 登入時，最後一步要拿 client secret 去換 token，這不能在瀏覽器做，所以需要一個小後端。
`oauth-proxy/` 就是它：Go 寫的單一 binary，沒有資料庫，閒置會自動關機（`min_machines_running = 0`）。

```bash
cd oauth-proxy
fly apps create peicheng-decap-oauth        # 名稱要跟 fly.toml 對得上
fly secrets set GITHUB_CLIENT_ID=xxx GITHUB_CLIENT_SECRET=yyy \
                ALLOWED_ORIGIN=https://peicheng0413.github.io
fly deploy
```

`ALLOWED_ORIGIN` 是安全閥：設了之後，只有這個網站拿得到 token。

完成後打開 `/admin/`，用 GitHub 登入就能編輯內容，存檔直接 commit 進 main，Actions 接手重建，約一分鐘後線上就更新了。

> 換帳號或換 repo 名稱時要一起改的地方：`config/_default/hugo.toml` 的 `baseURL`、
> `static/admin/config.yml` 的 `repo` 與 `base_url`、`oauth-proxy/fly.toml` 的 `app`。
> 線上的 baseURL 其實是 Actions 的 `configure-pages` 帶入的，本機那個值只影響你自己建置的結果。

## 內容結構

```
content/
├── _index.md              首頁主視覺（大標／副標／背景圖）
├── homepage/              首頁中間兩個橫幅區塊（headless bundle，不會單獨產生頁面）
├── products/              產品（示範的主角，6 筆）
├── services/              服務（featured: true 的會出現在首頁）
├── pages/about/           關於我們
└── contact/               聯絡我們

config/_default/
├── hugo.toml              建置設定，CMS 不碰
├── params.toml            站台參數，CMS 可改
└── menus.toml             導覽選單，CMS 可改
```

設定刻意拆成三個檔，是因為 Decap 存檔時會**整份重寫**它管理的檔案（註解會消失）。
把建置設定隔離在 `hugo.toml`，後台就算亂改也弄不壞網站建置。

## 這個站踩過的四個坑

**一、`relURL` 不會幫「以 `/` 開頭的路徑」補上子路徑。**
GitHub Pages 專案站的網址帶 `/repo-name/` 前綴，主題模板用的是 `{{ . | relURL }}`。
實測：`images/uploads/x.jpg` 會正確變成 `/hugo-decap-demo/images/uploads/x.jpg`，
但 `/images/uploads/x.jpg`（開頭有斜線）會原封不動輸出 → 線上 404。
所以 `config.yml` 的 `public_folder` 設成 **`images/uploads`（不加開頭斜線）**，
front matter 裡的圖片路徑也全部不帶斜線。改成部署在根網域的話這條就無所謂了。

**二、未來日期的內容不會被建置。**
Decap 新增文章時預設帶當下時間，若電腦時區／時間比伺服器快，可能出現「後台存了但網站上沒有」。
本站的示範資料就撞過一次。要放寬的話在建置指令加 `--buildFuture`。

**三、資料夾型集合會把 `_index.md` 也當成一筆資料。**
`content/products/_index.md` 其實是列表頁，不該出現在「產品」清單裡。
解法是給真正的產品加一個隱藏標記 `product: true`，集合再用 `filter` 只收有標記的檔案（服務同理）。

**四、Decap 寫 TOML 會重寫整份檔案。**
所以 CMS 能碰的 TOML 只給 `params.toml` 與 `menus.toml` 這兩個純資料檔，其餘設定放 `hugo.toml`。

## 授權

主題 [Hugo Hero](https://github.com/zerostaticthemes/hugo-hero-theme) 為 MIT 授權，授權條款保留在 `themes/hugo-hero-theme/LICENSE`。
本 repo 的內容為示範用途。
