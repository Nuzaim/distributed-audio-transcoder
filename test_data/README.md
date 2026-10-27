# Test Audio Data

This directory contains test audio fixtures used to verify the distributed audio transcoder application and worker pipeline.

## Sample Files

| File | Format | Duration | Purpose |
|------|--------|----------|---------|
| `sample_5s.mp3` | MP3 (MPEG Audio Layer III) | 5s | Single-segment HLS transcode (chunk size is 20s, produces 1 `.ts` segment) |
| `sample_25s.wav` | WAV (PCM 16-bit) | 25s | Multi-segment HLS transcode (produces `playlist0.ts` and `playlist1.ts`) |
| `sample_10s.aac` | AAC (Advanced Audio Coding) | 10s | Audio codec copy verification with AAC |
| `sample_silent_3s.mp3` | MP3 | 3s | Audio stream with silence/null signal (edge case) |

## Generating Test Data

You can regenerate or generate additional test audio files using the test runner:

```bash
# Force regenerate the default sample set
python3 scripts/test_app.py --generate

# Or generate using ffmpeg directly
ffmpeg -y -f lavfi -i "sine=frequency=440:duration=15" -c:a libmp3lame test_data/custom.mp3
```
