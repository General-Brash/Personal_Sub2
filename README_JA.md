# Personal_Sub2

Personal_Sub2 は、`1.6.0` のコードベースを基に個人で開発し、独立して保守しているバージョンです。

[English](README.md) | [中文](README_CN.md) | 日本語

## 個人版の機能

- **一時クレジット**：恒久残高とは別に、有効期限付きクレジットの付与、消費、利用可能額を記録します。
- **毎日のチェックイン**：設定可能なルールで一時クレジットを付与し、チェックイン状況と履歴を提供します。
- **銀行機能**：一時クレジットの前借りと、恒久残高から一時クレジットへの交換を、設定可能な上限、精算ルール、台帳記録とともに提供します。
- **セキュリティ監査の二次レビュー**：ASCII キーワードの境界照合を改善し、該当内容を独立した `intent-classifier` サービスで二次判定できます。`off`、`shadow`、`enforce` モードと、モデルパッケージの検証、アクティベーション、ロールバックに対応します。

本番用モデルの重みは含まれていません。モデルによる二次レビューを有効にする前に、[`MODEL_PACKAGE.md`](services/intent-classifier/MODEL_PACKAGE.md) に従ってモデルパッケージを用意し、アクティベートしてください。

## インストールとアップグレード

インストールスクリプトは、PostgreSQL と Redis が稼働している Linux amd64/arm64 サーバー向けで、root 権限が必要です：

```bash
curl -sSL https://raw.githubusercontent.com/General-Brash/Personal_Sub2/main/deploy/install.sh | sudo bash
```

インストール後、`http://YOUR_SERVER_IP:8080` を開いて初期設定を完了します。よく使うコマンド：

```bash
# 状態とログを確認
sudo systemctl status sub2api
sudo journalctl -u sub2api -f

# 個人リポジトリの最新 Release にアップグレード
curl -sSL https://raw.githubusercontent.com/General-Brash/Personal_Sub2/main/deploy/install.sh | sudo bash -s -- upgrade
```

管理画面からバージョン確認とアップグレードを行うこともできます。アップグレード前に、データベース、設定、データディレクトリをバックアップしてください。

個人版コンテナイメージの公開先：

```text
ghcr.io/general-brash/personal_sub2
```

デプロイファイルと実行時設定は [`deploy/`](deploy/) を参照してください。コンテナでデプロイする場合は、別バージョンとの混在を避けるため、アプリケーションイメージが上記の個人版イメージを明示的に参照していることを確認してください。

## ソースからのビルド

必要環境：Go 1.26.5、Node.js 20+、pnpm 9、PostgreSQL、Redis。

```bash
git clone https://github.com/General-Brash/Personal_Sub2.git
cd Personal_Sub2

cd frontend
pnpm install --frozen-lockfile
pnpm run build

cd ../backend
go build -tags embed -ldflags="-X main.Version=$(./scripts/resolve-version.sh)" -o sub2api ./cmd/server
./sub2api
```

初回起動後に `http://localhost:8080` を開き、セットアップウィザードでデータベース、Redis、管理者アカウントを設定してください。

## 開発と検証

```bash
# バックエンドテスト
cd backend
make test-unit

# フロントエンドチェック
cd ../frontend
pnpm run lint:check
pnpm run typecheck
pnpm run test:run
```

その他の開発規約は [`DEV_GUIDE.md`](DEV_GUIDE.md) を参照してください。

## セキュリティと利用責任

- 利用する地域の法令と、接続する各サービスの利用規約を確認してください。
- 本番環境では固有の強力なパスワードと固定シークレットを使用し、管理機能とデータベースへのネットワークアクセスを制限してください。
- API キー、アクセストークン、データベースパスワード、`.env` および `config.yaml` の機密情報をコミットまたは公開しないでください。
- アップグレード、移行、セキュリティポリシー変更の前にバックアップを取得し、非本番環境で検証してください。
- 本プロジェクトは現状のまま提供され、アカウント、サービス、データ、コンプライアンス上のリスクは利用者が負います。

## ライセンス

本プロジェクトは [GNU Lesser General Public License v3.0](LICENSE) またはそれ以降のバージョンでライセンスされています。
