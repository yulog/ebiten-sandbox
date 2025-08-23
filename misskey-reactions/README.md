# Floating Misskey Reactions

Misskeyの投稿に付けられたリアクションが、デスクトップ上を浮遊するデスクトップアクセサリーです。

ウィンドウは常に最前面に表示されますが、背景は透過され、マウスクリックも背後のウィンドウに透過します。作業の邪魔をすることなく、リアルタイムにリアクションを眺めて楽しむことができます。

## 主な機能

- Misskeyの投稿へのリアクションをリアルタイムに表示
- 標準絵文字、カスタム絵文字に対応
- GIFアニメーション絵文字の再生に対応
- 画像取得に失敗した場合、絵文字名をテキストで表示するフォールバック機能
- 常に最前面・背景透過・クリック透過表示
- Misskeyに接続せずに動作確認できるテストモード

## 必要なもの

- Go (1.20以上を推奨)
- Misskeyのアカウント

## セットアップ

1.  `config.json` ファイルを作成します。以下の内容をコピーしてください。

    ```json
    {
      "misskey_instance": "your.misskey.instance.com",
      "access_token": "YOUR_MISSKEY_ACCESS_TOKEN"
    }
    ```

2.  上記ファイルの内容を、ご自身の情報に書き換えます。
    - `misskey_instance`: あなたが利用しているMisskeyインスタンスのホスト名 (例: `misskey.io`)
    - `access_token`: あなたのMisskeyアカウントのアクセストークン。アクセストークンは、Misskeyの `設定` > `API` から取得できます。

3.  必要なライブラリをインストールします。

    ```bash
    go get ./...
    ```

## 使い方

### 通常モード

`config.json` で設定したアカウントに接続し、リアクションを待ち受けます。

```bash
go run .
```

### テストモード

Misskeyに接続せず、あらかじめ用意されたテストデータを順番に表示します。動作確認に便利です。

```bash
go run . -test
```

## 使用技術

- Go
- Ebitengine
- gorilla/websocket