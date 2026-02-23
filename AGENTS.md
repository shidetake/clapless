# AGENTS.md - Clapless Project Guide

## Project Overview

Clapless はダブルエンダー方式のポッドキャスト録音で、ローカル音源をミックス音源に自動同期するCLIツール。手を叩いて同期マーカーを作る手間を省く。

- **言語**: Go 1.25
- **ライセンス**: MIT
- **リポジトリ**: github.com/shidetake/clapless

## Architecture

```
cmd/clapless/main.go          # エントリーポイント（cli.Execute() を呼ぶだけ）
internal/
  cli/
    root.go                    # Cobra CLIフレームワーク、フラグ定義、バリデーション
    runner.go                  # メインワークフロー（ファイル読込→オフセット検出→ファイル書出）
    version.go                 # バージョン情報（ビルド時にldflagsで注入）
  audio/
    wav.go                     # WAV読み書き、モノラル変換、float64正規化
    silence.go                 # 無音生成・先頭追加
  sync/
    correlator.go              # FFTベース相互相関、ダウンサンプリング、正規化
    offset.go                  # パディング計算、信頼度検証、FileOffset構造体
    fine_tuner.go              # 2段階同期のファインチューニング
```

## Core Algorithm: 2-Stage Synchronization

### Stage 1: Coarse Detection（粗い検出）
1. ミックスとローカル音源をダウンサンプリング（デフォルト50倍）
2. 両信号をゼロ平均・単位分散に正規化
3. FFT相互相関で最適なオフセットを検出
4. 信頼度スコア算出（正規化ピーク値）

### Stage 2: Fine-Tuning（微調整）
1. 粗いアラインメント後、全ファイルにデータがある重複区間を特定
2. 重複区間から60秒セグメントを選択（最低30秒必要）
3. ダウンサンプリングなし（factor=1）で再度相互相関
4. 粗いオフセットに微調整を加算

### Offset Sign Convention（重要）
- **正のオフセット** = ローカル音源が遅れて始まった（ミックス途中から録音開始）→ 無音を先頭に追加
- **負のオフセット** = ローカル音源が早く始まった → 他のファイルに無音を追加
- FFT相互相関の結果: ピーク k > 0 → local が mix より k サンプル**後に**始まった
- ファインチューニングの調整値は `detectSegmentOffset` の結果を**そのまま**（符号反転なし）で加算: `FinalOffset = coarseOffset + headOffset`

### Padding Strategy
- 最小オフセットのファイルを基準（最も早いファイル）
- 他のファイルには `自身のオフセット - 最小オフセット` 分の無音を先頭に追加
- 非破壊：音声を削らず、無音追加のみ

## FFT Cross-Correlation Details

```
1. FFTサイズ = 2^ceil(log2(len(sig1)+len(sig2)-1))
2. FFT(signal1) * conj(FFT(signal2))  # 周波数領域での乗算
3. IFFT → 実数相関値
4. ピーク検索: 負のラグはインデックス fftSize-d に出現
5. peakIdx >= len(mixed) なら offset = peakIdx - fftSize（ラップアラウンド補正）
```

**注意**: FFT結果はfftSize全体を保持する。`n = len(s1)+len(s2)-1` にトリミングすると負のラグピークが失われる（commit 174ef54で修正）。

## CLI Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--mixed` | `-m` | (必須) | ミックス音源パス |
| `--segment-duration` | | 600 | 相関分析のセグメント長（秒） |
| `--downsample` | `-d` | 50 | 粗い検索のダウンサンプル係数 |
| `--version` | `-v` | | バージョン表示 |

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/go-audio/wav` | WAVコーデック |
| `github.com/go-audio/audio` | 音声フォーマット構造体 |
| `github.com/go-audio/riff` | RIFFチャンクパース |
| `github.com/spf13/cobra` | CLIフレームワーク |
| `gonum.org/v1/gonum` | FFT実装 |

## Development

### Build & Run
```bash
go build -o clapless ./cmd/clapless
./clapless -m mix.wav local1.wav local2.wav
```

### Test
```bash
go test ./...
```

### Release
- GitHubタグ (`v*`) をプッシュすると GitHub Actions + GoReleaser でリリース
- macOS/Linux/Windows × amd64/arm64 のバイナリ生成
- Homebrew tap (`shidetake/homebrew-tap`) に自動パブリッシュ

## Data Flow

```
CLI入力 → WAV読込 → モノラル変換 → サンプルレート検証
  → 並列オフセット検出（goroutine） → 粗いパディング計算
  → ファインチューニング → 最終パディング計算
  → 信頼度検証 → 同期ファイル書出（_synced.wav）
```

## Key Design Decisions & Lessons Learned

### 1. FFT結果のトリミング禁止
FFT結果を `n = len(s1)+len(s2)-1` にトリミングすると、fftSize > n のとき負のラグピーク（fftSize-d の位置）が失われる。ローカル音源がミックスより先に始まるケースで同期が壊れる。

### 2. ファインチューニング失敗時の出力保全
`FinetuneOffsets` が失敗しても、粗いオフセットで出力ファイルを生成する。3ファイル以上のケースで、ファインチューニング失敗が全出力を消失させるバグがあった（commit f812aad で修正）。

### 3. 符号規約の一貫性
オフセットの符号規約がコード全体で一貫していることが重要。
- `OffsetSamples > 0` = local が mix より後に始まった = 無音を追加する方向
- ファインチューニング: `fineAdjSamples = headOffset`（符号反転なし）で FinalOffset を補正
- ドリフトレート: `tailOffset < headOffset` = local クロックが速い → `driftRate = -(tailOffset-headOffset)/timeDiff > 0`（符号反転あり）

### 4. メモリ使用量
- 42分の音声（48kHz）: ペアあたり約1.5GB
- 60分の音声: ペアあたり約3.2GB
- 並列処理でファイル数倍になる

## File Naming Convention
- 出力ファイル: `{original_name}_synced.wav`
- 入力はWAVフォーマットのみ対応

## Python Utility Scripts（開発・検証用、gitignore外）
- `generate_test_data*.py`: テストデータ生成
- `generate_test_noise.py`: ノイズテストデータ生成
- `verify_synced.py` / `verify_test_data.py`: 同期結果の検証
- `debug_fft_size.py`: FFTサイズ・メモリ計算
- `calculate_memory.py`: メモリ使用量見積もり

## Confidence Score
- 閾値: 0.30（これ未満で警告表示）
- 計算: `peakValue / len(localNorm)`（正規化されたピーク値）
