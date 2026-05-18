你的 5 張表設計方向是對的：

```text
users
oauth_accounts
refresh_token_families
refresh_tokens
oauth_login_states
```

整體分工可以這樣看：

```text
users
  ├── oauth_accounts
  └── refresh_token_families
          └── refresh_tokens

oauth_login_states  -- OAuth callback 前的短期暫存狀態
```

這份 spec 的核心是：access token 是短命 JWT、hot path 不查 DB；refresh token 是 opaque token，存在 DB 裡，用來支援 refresh、logout、rotation、reuse detection；OAuth login 則需要用 `state` 串起 redirect URL、PKCE verifier、CSRF protection。

下面用 **PostgreSQL** 設計。

---

# 0. 前置建議

建議啟用：

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;
```

原因：

```text
pgcrypto -> gen_random_uuid()
citext   -> email 大小寫不敏感，例如 Foo@Email.com == foo@email.com
```

---

# 1. `users`

這張表代表「你系統內部的使用者」。

OAuth provider 是外部身份，`users` 是你自己系統的 user identity。

```sql
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    email CITEXT NOT NULL UNIQUE,
    display_name TEXT,
    avatar_url TEXT,

    role TEXT NOT NULL DEFAULT 'user',

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ,

    disabled_at TIMESTAMPTZ
);
```

## 欄位解釋

| 欄位              | 設計原因                                                                                           |
| --------------- | ---------------------------------------------------------------------------------------------- |
| `id`            | 系統內部 user id。access token 的 `sub` 可以放這個 UUID。不要用 email 當主鍵，因為 email 可能變。                       |
| `email`         | 使用者主要 email。OAuth callback 後會從 provider 的 `id_token` 或 profile 拿 email，接著 find or create user。 |
| `display_name`  | 顯示名稱，通常從 Google profile 的 name 來。                                                              |
| `avatar_url`    | 使用者頭像，通常從 OAuth profile 來。不是 auth 必需，但前端 `/users/me` 常會需要。                                     |
| `role`          | 基礎權限欄位，例如 `user`、`admin`。如果你之後有 RBAC，可以再拆更完整的 roles table。                                     |
| `created_at`    | user 建立時間。                                                                                     |
| `updated_at`    | user profile 更新時間。                                                                             |
| `last_login_at` | 最近一次成功登入時間，方便管理與 debug。                                                                        |
| `disabled_at`   | 軟停權。若不為 `NULL`，即使 token 合法，也可以拒絕敏感操作。                                                          |

### 為什麼 `users` 不存 refresh token？

因為一個 user 可以有多個登入 session，例如：

```text
Chrome on Mac
Safari on iPhone
Lab computer
```

每個 session 都應該有自己的 refresh token family，所以 token 狀態不能直接塞在 `users` 裡。

---

# 2. `oauth_accounts`

這張表代表「某個 user 綁定的 OAuth 身份」。

```sql
CREATE TABLE oauth_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    provider TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,

    provider_email CITEXT NOT NULL,
    email_verified BOOLEAN NOT NULL DEFAULT false,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ,

    UNIQUE (provider, provider_user_id)
);
```

## 欄位解釋

| 欄位                 | 設計原因                                                                                                           |
| ------------------ | -------------------------------------------------------------------------------------------------------------- |
| `id`               | OAuth account 自己的主鍵。                                                                                           |
| `user_id`          | 對應到你系統內部的 `users.id`。一個 user 可以綁多個 OAuth provider。                                                             |
| `provider`         | OAuth provider 名稱，例如 `google`、`github`、`microsoft`。你的 spec 有說 Google 只是 example，未來可能加 provider，所以不要 hard-code。 |
| `provider_user_id` | provider 給的穩定 user id。對 Google 來說通常是 OpenID Connect 裡的 `sub`。這比 email 更適合當外部身份識別。                              |
| `provider_email`   | provider 回傳的 email。注意它不一定永遠不變，所以不要只靠這個找 OAuth account。                                                         |
| `email_verified`   | provider 是否確認該 email 已驗證。Google OIDC 會有類似資訊。                                                                   |
| `created_at`       | 這個 OAuth identity 第一次綁定時間。                                                                                     |
| `updated_at`       | provider profile 資料更新時間。                                                                                       |
| `last_login_at`    | 這個 provider account 最近登入時間。                                                                                    |

## Constraint 解釋

```sql
UNIQUE (provider, provider_user_id)
```

這非常重要。

原因是：

```text
同一個 Google sub 只能對應到一個系統 user
```

否則同一個 Google 帳號可能登入成兩個不同 user，auth 直接裂開，資料庫當場開派對，不是好派對。

### 要不要加 `UNIQUE (user_id, provider)`？

看產品需求。

如果你希望一個 user 最多綁一個 Google account，可以加：

```sql
ALTER TABLE oauth_accounts
ADD CONSTRAINT oauth_accounts_user_provider_unique
UNIQUE (user_id, provider);
```

如果允許同一個 user 綁多個 Google 帳號，就不要加。

---

# 3. `refresh_token_families`

這張表代表「一次 login 產生的一整條 refresh token chain」。

你的 spec 裡面 refresh token 會 rotate：

```text
R1 -> R2 -> R3 -> ...
```

logout 時不是只 revoke 當前 refresh token，而是 revoke 整個 family。這就是為什麼需要 `refresh_token_families`。

```sql
CREATE TABLE refresh_token_families (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    oauth_account_id UUID REFERENCES oauth_accounts(id) ON DELETE SET NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,

    revoked_at TIMESTAMPTZ,
    revoked_reason TEXT,

    reuse_detected_at TIMESTAMPTZ,

    last_used_at TIMESTAMPTZ,

    ip_address INET,
    user_agent TEXT
);
```

## 欄位解釋

| 欄位                  | 設計原因                                                                                      |
| ------------------- | ----------------------------------------------------------------------------------------- |
| `id`                | refresh token family id。每次登入成功建立一個新的 family。                                              |
| `user_id`           | 這條 refresh chain 屬於哪個 user。                                                               |
| `oauth_account_id`  | 這次登入是透過哪個 OAuth account 完成的。設成 nullable，因為未來如果加 password login、magic link login，也可以共用這張表。 |
| `created_at`        | family 建立時間，也就是這次登入 session 的開始時間。                                                        |
| `expires_at`        | 整個 family 的絕對過期時間。根據你的設計，refresh token 是 30 天 non-sliding，所以不是每次 refresh 就延長 30 天。        |
| `revoked_at`        | 整個 family 被撤銷的時間。logout 時會設這個欄位。                                                          |
| `revoked_reason`    | 為什麼 revoke，例如 `logout`、`reuse_detected`、`admin_revoked`、`user_disabled`。                  |
| `reuse_detected_at` | 如果發現舊 refresh token 被重複使用，記錄偵測時間。這通常代表 token 可能外洩或 multi-tab coordination 有 bug。          |
| `last_used_at`      | 最近一次 refresh 成功時間。方便 session 管理頁面或 debug。                                                 |
| `ip_address`        | 建立 family 時的 IP。可用於安全審計。                                                                  |
| `user_agent`        | 建立 family 時的 browser/device 資訊。可以顯示「目前登入裝置」。                                              |

### 為什麼 `expires_at` 放在 family，而不是只放在 token？

因為你的 refresh token 是 **non-sliding 30 days**。

也就是：

```text
Mon 10:00 login
family expires_at = 30 天後

Mon 10:14 refresh -> 新 token
Mon 10:28 refresh -> 新 token
...
但 family expires_at 不變
```

所以「絕對過期時間」是 family 的屬性，不是單顆 refresh token 自己一直往後延長。

這點很重要，別寫成每次 refresh 都 `now() + 30 days`，不然就變 sliding session 了。

---

# 4. `refresh_tokens`

這張表代表「每一顆 refresh token」。

注意：DB 裡只存 hash，不存明文 token。你的 spec 明確要求 refresh token 是 opaque token，server 用 `sha256(token)` 查 DB，而且舊 token 要保留來偵測 replay。

```sql
CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    family_id UUID NOT NULL REFERENCES refresh_token_families(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    token_hash BYTEA NOT NULL UNIQUE,

    rotated_from_token_id UUID REFERENCES refresh_tokens(id) ON DELETE SET NULL,

    issued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT refresh_tokens_not_used_before_issued
        CHECK (used_at IS NULL OR used_at >= issued_at),

    CONSTRAINT refresh_tokens_not_revoked_before_issued
        CHECK (revoked_at IS NULL OR revoked_at >= issued_at)
);
```

## 欄位解釋

| 欄位                      | 設計原因                                                                                        |
| ----------------------- | ------------------------------------------------------------------------------------------- |
| `id`                    | 單顆 refresh token 的主鍵。                                                                       |
| `family_id`             | 這顆 token 屬於哪一條 refresh chain。refresh 時新 token 會沿用同一個 `family_id`。                           |
| `user_id`               | 冗餘欄位，但很實用。可以不用 join family 就知道 token 屬於哪個 user。缺點是要保證與 family 的 user 一致。                    |
| `token_hash`            | refresh token 的 hash，例如 `sha256(raw_refresh_token)`。不要存明文 token，因為 DB 洩漏時明文 token 可以直接拿來登入。 |
| `rotated_from_token_id` | 這顆 token 是從哪一顆舊 token rotate 來的。方便追蹤 token chain：`R1 -> R2 -> R3`。                          |
| `issued_at`             | 這顆 token 簽發時間。                                                                              |
| `used_at`               | 這顆 token 被 refresh 消耗掉的時間。舊 token 不刪掉，而是設 `used_at`，才能偵測 reuse。                             |
| `revoked_at`            | 單顆 token 被撤銷的時間。一般 logout 會 revoke family，不一定需要逐顆 revoke，但這欄位保留彈性。                          |
| `created_at`            | row 建立時間，通常等於 `issued_at`，但保留語意清楚。                                                          |

## 為什麼需要 `used_at`？

refresh token rotation 的核心就是：

```text
第一次使用 R1：
    R1.used_at = now()
    insert R2

第二次又有人拿 R1 來 refresh：
    發現 R1.used_at IS NOT NULL
    => reuse detected
    => 401
    => 清 cookies / revoke family
```

這就是防止 refresh token 被偷後長期使用的關鍵。

## 為什麼 `refresh_tokens` 不一定需要 `expires_at`？

我會把絕對過期時間放在 `refresh_token_families.expires_at`。

refresh 時查法會像：

```sql
SELECT
    rt.*,
    rtf.expires_at AS family_expires_at,
    rtf.revoked_at AS family_revoked_at
FROM refresh_tokens rt
JOIN refresh_token_families rtf ON rtf.id = rt.family_id
WHERE rt.token_hash = $1;
```

然後判斷：

```text
查不到                         => 401
family_expires_at < now()       => 401
family_revoked_at IS NOT NULL   => 401
rt.revoked_at IS NOT NULL       => 401
rt.used_at IS NOT NULL          => reuse detected
正常                            => rotate
```

這樣比較不會出現 token row 和 family row 的過期時間不同步。

---

# 5. `oauth_login_states`

這張表是 OAuth login 中途的暫存狀態。

它負責把這幾件事綁在一起：

```text
state
redirect_url
PKCE code_verifier
provider
CSRF protection
```

你的 spec 裡面說，`state` 是 verifier lookup、redirect URL preservation、CSRF protection 的 join key，而且必須 single-use。

```sql
CREATE TABLE oauth_login_states (
    state_hash BYTEA PRIMARY KEY,

    provider TEXT NOT NULL,

    code_verifier_hash BYTEA NOT NULL,

    redirect_url TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,

    ip_address INET,
    user_agent TEXT,

    CONSTRAINT oauth_login_states_not_used_before_created
        CHECK (used_at IS NULL OR used_at >= created_at),

    CONSTRAINT oauth_login_states_expire_after_created
        CHECK (expires_at > created_at)
);
```

## 欄位解釋

| 欄位                   | 設計原因                                                                                                                         |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `state_hash`         | OAuth `state` 的 hash。callback 時拿 request 裡的 `state` 做 hash 查 DB。建議不要存明文 state，因為它是一次性安全值。                                    |
| `provider`           | 這次 OAuth login 用哪個 provider，例如 `google`。callback 時可以知道要用哪個 provider 設定交換 token。                                              |
| `code_verifier_hash` | PKCE 的 verifier。嚴格來說，callback 時你需要原始 `code_verifier` 傳給 provider，所以如果存在 DB，你通常需要可取回原文。若只存 hash，callback 不能交換 token。下面我會特別說明。 |
| `redirect_url`       | 登入完成後要導回前端哪個頁面，也就是一開始 `/login/oauth/google?r=...` 的 `r`。                                                                     |
| `created_at`         | state 建立時間。                                                                                                                  |
| `expires_at`         | state 過期時間。通常設 5 到 10 分鐘。OAuth round-trip 不應該活太久。                                                                            |
| `used_at`            | callback 成功或失敗後都應該標記 used，避免同一個 state 被重放。                                                                                   |
| `ip_address`         | 建立 state 時的 IP，用於安全審計。                                                                                                       |
| `user_agent`         | 建立 state 時的 user-agent，用於 debug 或簡單風險檢查。                                                                                     |

## 重要修正：`code_verifier` 不能只存 hash

這裡要小心。

PKCE callback 時，你必須把原始 `code_verifier` 送給 OAuth provider：

```text
code_verifier={code_verifier}
```

所以如果存在 SQL DB，單純存 `code_verifier_hash` 是不夠的，因為 hash 無法還原。

實務上有兩種做法。

### 做法 A：DB 存明文 verifier，但嚴格短期、single-use

```sql
CREATE TABLE oauth_login_states (
    state_hash BYTEA PRIMARY KEY,

    provider TEXT NOT NULL,

    code_verifier TEXT NOT NULL,
    redirect_url TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,

    ip_address INET,
    user_agent TEXT
);
```

這是 MVP 最簡單做法。

但要注意：

```text
code_verifier 是 one-shot secret
不要 log
TTL 要短
callback 後立刻刪除或標記 used
```

### 做法 B：用 Redis 存 verifier

比較推薦：

```text
key   = oauth_state:{sha256(state)}
value = {
  provider,
  code_verifier,
  redirect_url,
  created_at
}
TTL   = 5~10 minutes
```

原因是這份資料不是長期業務資料，它只是 OAuth 流程中途的暫存狀態。DB 也可以，但 Redis 更符合生命週期。

如果你現在還沒接 Redis，用 DB 先做完全 OK。不要為了架構潔癖卡住，auth 先跑通比較香。

---

# 建議加的 indexes

## `oauth_accounts`

```sql
CREATE INDEX idx_oauth_accounts_user_id
ON oauth_accounts(user_id);
```

用途：

```text
查某個 user 綁了哪些 provider
```

---

## `refresh_token_families`

```sql
CREATE INDEX idx_refresh_token_families_user_id
ON refresh_token_families(user_id);

CREATE INDEX idx_refresh_token_families_active
ON refresh_token_families(user_id)
WHERE revoked_at IS NULL;
```

用途：

```text
查使用者目前有哪些 active sessions
```

---

## `refresh_tokens`

```sql
CREATE INDEX idx_refresh_tokens_family_id
ON refresh_tokens(family_id);

CREATE INDEX idx_refresh_tokens_user_id
ON refresh_tokens(user_id);

CREATE INDEX idx_refresh_tokens_used_at
ON refresh_tokens(used_at);
```

`token_hash` 已經 `UNIQUE`，所以它本身會有 index。

最重要查詢是：

```sql
SELECT ...
FROM refresh_tokens
WHERE token_hash = $1;
```

因此 `token_hash UNIQUE` 是必要的。

---

## `oauth_login_states`

```sql
CREATE INDEX idx_oauth_login_states_expires_at
ON oauth_login_states(expires_at);

CREATE INDEX idx_oauth_login_states_used_at
ON oauth_login_states(used_at);
```

用途：

```text
定期清掉 expired / used state
```

例如：

```sql
DELETE FROM oauth_login_states
WHERE expires_at < now()
   OR used_at IS NOT NULL;
```

---

# Refresh 流程對應資料表操作

## Login 成功

```text
1. find or create users
2. find or create oauth_accounts
3. create refresh_token_families
4. create refresh_tokens 第一顆 token
5. set access_token cookie
6. set refresh_token cookie
```

---

## Refresh

```text
1. hash cookie 裡的 refresh_token
2. 查 refresh_tokens.token_hash
3. join refresh_token_families
4. 檢查 family 是否 expired/revoked
5. 檢查 token 是否 revoked
6. 檢查 token.used_at
7. 若 used_at 不為 NULL => reuse detected
8. 若正常：
   - old token.used_at = now()
   - insert new refresh_tokens row
   - refresh_token_families.last_used_at = now()
   - set new access_token cookie
   - set new refresh_token cookie
```

---

## Logout

```text
1. hash refresh_token cookie
2. 找到 refresh_tokens
3. 找到 family_id
4. refresh_token_families.revoked_at = now()
5. refresh_token_families.revoked_reason = 'logout'
6. clear cookies
```

---

# 我會給你的最終 schema 版本

如果整理成一版，我會這樣寫：

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    email CITEXT NOT NULL UNIQUE,
    display_name TEXT,
    avatar_url TEXT,

    role TEXT NOT NULL DEFAULT 'user',

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ,

    disabled_at TIMESTAMPTZ
);

CREATE TABLE oauth_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    provider TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,

    provider_email CITEXT NOT NULL,
    email_verified BOOLEAN NOT NULL DEFAULT false,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ,

    UNIQUE (provider, provider_user_id)
);

CREATE TABLE refresh_token_families (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    oauth_account_id UUID REFERENCES oauth_accounts(id) ON DELETE SET NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,

    revoked_at TIMESTAMPTZ,
    revoked_reason TEXT,

    reuse_detected_at TIMESTAMPTZ,

    last_used_at TIMESTAMPTZ,

    ip_address INET,
    user_agent TEXT
);

CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    family_id UUID NOT NULL REFERENCES refresh_token_families(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    token_hash BYTEA NOT NULL UNIQUE,

    rotated_from_token_id UUID REFERENCES refresh_tokens(id) ON DELETE SET NULL,

    issued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT refresh_tokens_not_used_before_issued
        CHECK (used_at IS NULL OR used_at >= issued_at),

    CONSTRAINT refresh_tokens_not_revoked_before_issued
        CHECK (revoked_at IS NULL OR revoked_at >= issued_at)
);

CREATE TABLE oauth_login_states (
    state_hash BYTEA PRIMARY KEY,

    provider TEXT NOT NULL,

    code_verifier TEXT NOT NULL,
    redirect_url TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,

    ip_address INET,
    user_agent TEXT,

    CONSTRAINT oauth_login_states_not_used_before_created
        CHECK (used_at IS NULL OR used_at >= created_at),

    CONSTRAINT oauth_login_states_expire_after_created
        CHECK (expires_at > created_at)
);

CREATE INDEX idx_oauth_accounts_user_id
ON oauth_accounts(user_id);

CREATE INDEX idx_refresh_token_families_user_id
ON refresh_token_families(user_id);

CREATE INDEX idx_refresh_token_families_active
ON refresh_token_families(user_id)
WHERE revoked_at IS NULL;

CREATE INDEX idx_refresh_tokens_family_id
ON refresh_tokens(family_id);

CREATE INDEX idx_refresh_tokens_user_id
ON refresh_tokens(user_id);

CREATE INDEX idx_oauth_login_states_expires_at
ON oauth_login_states(expires_at);
```

---

# 小結

你的 5 張表可以這樣定位：

| Table                    | 目的                                                |
| ------------------------ | ------------------------------------------------- |
| `users`                  | 系統內部 user identity                                |
| `oauth_accounts`         | OAuth provider 身份綁定                               |
| `refresh_token_families` | 一次 login session / refresh token chain            |
| `refresh_tokens`         | 每一顆 refresh token 的 hash、rotation、reuse detection |
| `oauth_login_states`     | OAuth + PKCE callback 前的短期 single-use state       |

最重要的設計原則是：

```text
access token 不進 DB
refresh token 只存 hash
refresh token family 控制 30 天 non-sliding 生命週期
logout revoke family
oauth state 必須 single-use 且短期有效
```

這樣設計其實已經蠻乾淨了，auth 這塊最怕「現在好像能跑，但安全語意混在一起」。你現在這版把 user、provider identity、session family、single token、OAuth transient state 拆開，之後維護起來會舒服很多。