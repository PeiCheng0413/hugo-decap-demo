/**
 * Decap CMS 的 GitHub OAuth 中繼站（Cloudflare Worker）
 *
 * 為什麼需要它：GitHub 的 OAuth 流程最後一步要用 client_secret 去換 access token，
 * 而 secret 不能放在瀏覽器裡，所以必須有一個後端代勞。這支 Worker 只做這件事，
 * 不存任何資料。
 *
 * 兩個路由：
 *   GET /auth      → 把使用者導去 GitHub 授權頁
 *   GET /callback  → GitHub 帶著 code 回來，換成 token 後用 postMessage 丟回 /admin 視窗
 *
 * 需要的環境變數（用 wrangler secret put 設定）：
 *   GITHUB_CLIENT_ID、GITHUB_CLIENT_SECRET
 * 選用：
 *   ALLOWED_ORIGIN  只允許這個網站來要 token，例如 https://peicheng0413.github.io
 */

const GITHUB_AUTHORIZE = 'https://github.com/login/oauth/authorize';
const GITHUB_TOKEN = 'https://github.com/login/oauth/access_token';

export default {
  async fetch(request, env) {
    const url = new URL(request.url);

    if (url.pathname === '/auth') {
      return handleAuth(url, env);
    }
    if (url.pathname === '/callback') {
      return handleCallback(request, url, env);
    }
    return new Response('Decap CMS OAuth proxy. 可用路由：/auth、/callback', {
      headers: { 'Content-Type': 'text/plain; charset=utf-8' },
    });
  },
};

function handleAuth(url, env) {
  const state = crypto.randomUUID();
  const authorize = new URL(GITHUB_AUTHORIZE);
  authorize.searchParams.set('client_id', env.GITHUB_CLIENT_ID);
  authorize.searchParams.set('redirect_uri', `${url.origin}/callback`);
  // Decap 會帶 scope 進來；沒帶就用 repo（私有 repo 也能用）
  authorize.searchParams.set('scope', url.searchParams.get('scope') || 'repo,user');
  authorize.searchParams.set('state', state);

  return new Response(null, {
    status: 302,
    headers: {
      Location: authorize.toString(),
      // state 存在 cookie，callback 回來時比對，擋 CSRF
      'Set-Cookie': `decap_state=${state}; HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=600`,
    },
  });
}

async function handleCallback(request, url, env) {
  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  const cookie = request.headers.get('Cookie') || '';
  const saved = /decap_state=([^;]+)/.exec(cookie)?.[1];

  if (!code || !state || state !== saved) {
    return renderResult({ error: 'state 不符或缺少 code，請重新登入一次' }, env);
  }

  const res = await fetch(GITHUB_TOKEN, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
      'User-Agent': 'decap-cms-oauth-worker',
    },
    body: JSON.stringify({
      client_id: env.GITHUB_CLIENT_ID,
      client_secret: env.GITHUB_CLIENT_SECRET,
      code,
    }),
  });

  const data = await res.json();
  if (!data.access_token) {
    return renderResult({ error: data.error_description || '拿不到 access token' }, env);
  }
  return renderResult({ token: data.access_token }, env);
}

/**
 * Decap 的握手協定：
 *   1. 這個彈出視窗先對 opener 喊一聲 "authorizing:github"
 *   2. opener 回一則訊息，我們才把結果丟回去（丟回它的 origin，不亂噴）
 */
function renderResult(result, env) {
  const payload = result.token
    ? `authorization:github:success:${JSON.stringify({ token: result.token, provider: 'github' })}`
    : `authorization:github:error:${JSON.stringify({ message: result.error })}`;

  const allowed = env.ALLOWED_ORIGIN || '';

  const html = `<!doctype html>
<html lang="zh-Hant"><head><meta charset="utf-8"><title>登入中…</title></head>
<body style="font-family: system-ui; padding: 2rem">
<p>${result.token ? '登入成功，可以關掉這個視窗了。' : '登入失敗：' + result.error}</p>
<script>
  (function () {
    var payload = ${JSON.stringify(payload)};
    var allowed = ${JSON.stringify(allowed)};
    function send(origin) {
      if (allowed && origin !== allowed) return;
      window.opener.postMessage(payload, origin);
    }
    window.addEventListener('message', function (e) { send(e.origin); }, false);
    window.opener.postMessage('authorizing:github', allowed || '*');
  })();
</script>
</body></html>`;

  return new Response(html, {
    headers: {
      'Content-Type': 'text/html; charset=utf-8',
      // 用過即丟
      'Set-Cookie': 'decap_state=; HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=0',
    },
  });
}
