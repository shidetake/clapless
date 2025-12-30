# Clapless

ダブルエンダー方式のポッドキャスト録音で、ローカル音源を自動同期するツールです。手を叩いて同期マーカーを作る手間を省きます。

## 特徴

- **自動同期**: 相互相関アルゴリズムで音声のオフセットを自動検出
- **非破壊**: 元の音声データは削らず、早いファイルに無音を追加
- **高速**: Goによる実装とgoroutineによる並列処理
- **シンプル**: WAVファイルのみに対応したシンプルな仕様

## インストール

### ソースからビルド

```bash
git clone https://github.com/shidetake/clapless.git
cd clapless
go build -o clapless ./cmd/clapless
```

### バイナリを配置

```bash
# macOS/Linux
sudo mv clapless /usr/local/bin/

# またはパスの通った任意の場所に配置
```

## 使い方

### 基本的な使い方

```bash
clapless --mixed <ミックス音源.wav> <ローカル音源1.wav> <ローカル音源2.wav> [...]
```

### 例

```bash
# 2人のポッドキャスト
clapless --mixed podcast_mix.wav alice.wav bob.wav

# 3人のポッドキャスト
clapless --mixed podcast_mix.wav alice.wav bob.wav charlie.wav
```

### 出力

同期された音源ファイルが `_synced` サフィックス付きで生成されます：

```
alice_synced.wav
bob_synced.wav
charlie_synced.wav
```

## 出力例

```
Clapless - Audio Synchronization Tool
======================================

Loading files...
  ✓ Mixed: podcast_mix.wav (2 channels, 44100 Hz, 45:32)
  ✓ Local 1: alice.wav (1 channel, 44100 Hz, 45:32)
  ✓ Local 2: bob.wav (1 channel, 44100 Hz, 45:32)

Detecting offsets...
  ✓ alice.wav: +0.234s (confidence: 0.92)
  ✓ bob.wav: +1.102s (confidence: 0.89)

Calculating synchronization...
  alice.wav: Adding 0.868s silence
  bob.wav: No padding needed (earliest)

Writing synchronized files...
  ✓ alice_synced.wav
  ✓ bob_synced.wav

Synchronization complete!
```

## 仕組み

1. **音声読み込み**: ミックス音源と各ローカル音源を読み込み
2. **オフセット検出**: FFTベースの相互相関で各ローカル音源のオフセットを並列検出
3. **無音計算**: 最も早いファイルを基準に、他のファイルに追加する無音の長さを計算
4. **同期ファイル生成**: 無音を追加した新しいWAVファイルを生成

### アルゴリズム

- **相互相関**: FFT（高速フーリエ変換）を使用した効率的な相互相関計算（O(N log N)）
- **信号正規化**: 振幅の違いを吸収するため、信号を正規化してから相互相関を計算
- **信頼度スコア**: 検出したオフセットの信頼性をスコアとして表示

## 要件

- **入力**: WAVフォーマットのみ対応
- **最低ファイル数**: ミックス音源1つ + ローカル音源2つ以上
- **サンプルレート**: 全てのファイルが同じサンプルレートである必要があります

## トラブルシューティング

### サンプルレート不一致エラー

```
Error: sample rate mismatch: mixed (48000 Hz) vs local 1 (44100 Hz)
```

全てのWAVファイルを同じサンプルレートに変換してください。ffmpegを使った例：

```bash
ffmpeg -i input.wav -ar 44100 output.wav
```

### 低い信頼度スコア

信頼度スコアが0.3未満の場合、警告が表示されます：

```
⚠️  Warnings:
  alice.wav: low confidence score 0.25 (threshold: 0.30)
  Synchronization may not be accurate. Please verify results.
```

以下の原因が考えられます：

- ノイズが多い
- 音声の重なりが少ない
- 音質が大きく異なる

手動で確認するか、録音環境を改善してください。

### ファイルが存在しないエラー

```
Error: mixed file error: file does not exist: podcast_mix.wav
```

ファイルパスが正しいか確認してください。相対パスまたは絶対パスで指定できます。

## 技術スタック

- **言語**: Go
- **音声処理**: [go-audio/wav](https://github.com/go-audio/wav)
- **信号処理**: [Gonum](https://www.gonum.org/)
- **並列処理**: Goroutines

## ライセンス

MIT License

## 貢献

Issue、Pull Requestは歓迎します。

## 開発

### テスト実行

```bash
go test ./...
```

### ビルド（クロスコンパイル）

```bash
# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o clapless-darwin-arm64 ./cmd/clapless

# macOS (Intel)
GOOS=darwin GOARCH=amd64 go build -o clapless-darwin-amd64 ./cmd/clapless

# Linux
GOOS=linux GOARCH=amd64 go build -o clapless-linux-amd64 ./cmd/clapless

# Windows
GOOS=windows GOARCH=amd64 go build -o clapless-windows-amd64.exe ./cmd/clapless
```

## 参考

- [Cross-Correlation](https://en.wikipedia.org/wiki/Cross-correlation)
- [Fast Fourier Transform (FFT)](https://en.wikipedia.org/wiki/Fast_Fourier_transform)
