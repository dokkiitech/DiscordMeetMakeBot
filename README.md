# DiscordMeetMakeBot

Go 製の Discord Bot。スラッシュコマンド `/meet` で Google Meet の URL を発行します。
内部では Google Calendar API でカレンダーイベント（既定 30 分）を作成し、付随する Meet 会議リンクを返却します。

## コマンド

- `/meet [title] [minutes]`
  - `title` (任意): 会議タイトル
  - `minutes` (任意): 会議時間（分、既定 30）

## 必要な環境変数

`.env.example` を参照してください。

| 変数 | 必須 | 説明 |
| --- | --- | --- |
| `DISCORD_BOT_TOKEN` | ○ | Discord Developer Portal で作成した Bot のトークン |
| `DISCORD_GUILD_ID` | × | 指定するとそのギルドのみへ即時にコマンド登録。未指定はグローバル登録 |
| `GOOGLE_CLIENT_ID` | ○ | Google Cloud OAuth クライアント ID |
| `GOOGLE_CLIENT_SECRET` | ○ | Google Cloud OAuth クライアントシークレット |
| `GOOGLE_REFRESH_TOKEN` | ○ | Calendar `events` スコープを許可した Refresh Token |
| `GOOGLE_CALENDAR_ID` | × | 既定 `primary` |

## Google 側の準備

1. Google Cloud Console でプロジェクトを作り、`Google Calendar API` を有効化。
2. OAuth 同意画面を設定し、`Desktop app` 種別の OAuth クライアントを作成。
3. `https://www.googleapis.com/auth/calendar.events` スコープのリフレッシュトークンを取得。
   （例: [oauth2l](https://github.com/google/oauth2l) や OAuth Playground を利用）
4. 取得した Refresh Token を `GOOGLE_REFRESH_TOKEN` に設定。

## Discord 側の準備

1. https://discord.com/developers/applications で Application を作成。
2. Bot を追加し Token を取得 (`DISCORD_BOT_TOKEN`)。
3. OAuth2 → URL Generator で `bot` と `applications.commands` を選択し、ギルドへ招待。

## ローカル実行

```bash
cp .env.example .env
# .env に必要な値を埋める
export $(grep -v '^#' .env | xargs)
go run .
```

## Docker

```bash
docker build -t discord-meet-bot .
docker run --rm \
  -e DISCORD_BOT_TOKEN \
  -e DISCORD_GUILD_ID \
  -e GOOGLE_CLIENT_ID \
  -e GOOGLE_CLIENT_SECRET \
  -e GOOGLE_REFRESH_TOKEN \
  -e GOOGLE_CALENDAR_ID \
  discord-meet-bot
```

## Coolify でのデプロイ

1. Coolify で新規 Application を作成し、本リポジトリを指定。
2. Build Pack を `Dockerfile` に設定（リポジトリ直下の `Dockerfile` を自動検出）。
3. Environment Variables に上記の値を設定。
4. Deploy を実行。Bot は永続プロセスとして稼働します（公開ポート不要）。
