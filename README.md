# DiscordMeetMakeBot

Go 製の Discord Bot。スラッシュコマンド `/meet` で Google Meet の URL を発行します。
内部では Google Calendar API でカレンダーイベント（既定 30 分）を作成し、付随する Meet 会議リンクを返却します。

## コマンド

- `/meet 招待:<@user1 @user2 ...> [タイトル:<文字列>] [会議時間:<数値>] [日時:<開始日時>]`
  - `招待` (必須): 招待する Discord ユーザーを @メンションで指定。スペース区切りで複数指定でき、人数の上限はありません。
  - `タイトル` (任意): 会議タイトル
  - `会議時間` (任意): 会議時間（分、既定 30）
  - `日時` (任意): 予約する開始日時（JST）。未指定の場合は即時発行です。

### 即時発行（`日時` 未指定）

依頼者と招待された各ユーザーの DM に Meet リンクが送信され、コマンドを実行したチャンネルには「DM に Meet リンクを送信しました」と通知されます。DM 受信を許可していないユーザーには送信されないため、その旨もチャンネル通知に含まれます。

### 予約（`日時` 指定）

`日時` に未来の開始時刻を指定すると、その時刻でカレンダー予定を作成し、開始までの残り時間に応じて参加者全員の DM にリマインドします。

- **30 分以下先**: これまでどおり即時に Meet リンクを DM で送信します（メッセージに開始時刻を追記）。
- **30 分より先〜2 日未満**: 予約作成を DM で通知し、**開始 30 分前**にリマインド（このリマインドで Meet リンクを送信）。
- **2 日以上先**: 予約作成を DM で通知し、**開始 1 日前**と**開始 30 分前**にリマインド（Meet リンクは 30 分前のリマインドで送信）。

`日時` の書式（いずれも JST として解釈）:

| 例 | 意味 |
| --- | --- |
| `2026-07-10 15:00` / `2026/07/10 15:00` | 年月日と時刻 |
| `07-10 15:00` / `07/10 15:00` | 今年の月日と時刻（過ぎていれば翌年） |
| `15:00` | 今日の時刻（過ぎていれば翌日） |

> リマインドはボットプロセス内のタイマーで管理します。再起動をまたいで保持したい場合は `SCHEDULE_STORE_PATH` に永続パス（ボリューム）を指定してください（未指定時はメモリ上のみ）。

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
| `SCHEDULE_STORE_PATH` | × | 予約リマインドの永続化ファイルパス。指定すると再起動をまたいでリマインドを保持（未指定はメモリ上のみ） |

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
  -e SCHEDULE_STORE_PATH \
  discord-meet-bot
```

## Coolify でのデプロイ

1. Coolify で新規 Application を作成し、本リポジトリを指定。
2. Build Pack を `Dockerfile` に設定（リポジトリ直下の `Dockerfile` を自動検出）。
3. Environment Variables に上記の値を設定。
4. Deploy を実行。Bot は永続プロセスとして稼働します（公開ポート不要）。
