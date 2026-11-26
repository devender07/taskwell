"""Narrates the video with Kokoro (open-source, Apache 2.0, runs offline): one line per scene into public/voice/<scene>.wav,
which Sound.tsx plays as each scene starts. A line longer than its scene is sped up (up to 1.25x) to fit.

Setup once, from video/ (about 340 MB):
  python3 -m venv .venv && .venv/bin/pip install kokoro-onnx soundfile
  mkdir -p .voice && for f in kokoro-v1.0.onnx voices-v1.0.bin; do
    curl -fL -o .voice/$f https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.0/$f; done
Then:  npm run voice                       (voice af_heart)
       npm run voice -- --voice am_michael (af_heart, af_bella, am_michael, am_adam, bf_emma, bm_george, ...)
"""
import argparse
import json
import pathlib
import subprocess

import soundfile as sf
from kokoro_onnx import Kokoro

HERE = pathlib.Path(__file__).resolve().parent.parent
LINES = json.loads((HERE / "src/narration.json").read_text())
LEAD_IN = 0.25  # seconds after the scene starts before the line begins (Sound.tsx: VOICE_DELAY)
GAP = 0.35  # seconds of quiet kept before the next scene starts

# The scenes and their on-screen seconds, from src/timing.ts, so the lines fit the video as it is.
timing = json.loads(subprocess.check_output(
    ["node", "--experimental-strip-types", "--disable-warning=ExperimentalWarning", "--input-type=module", "-e",
     "import { FPS, SCENES, SHOWN } from './src/timing.ts';"
     "console.log(JSON.stringify(SCENES.map(([n], i) => [n, SHOWN[i] / FPS])))"],
    cwd=HERE, text=True))

parser = argparse.ArgumentParser()
parser.add_argument("--voice", default="af_heart")
parser.add_argument("--speed", type=float, default=1.0)
args = parser.parse_args()

kokoro = Kokoro(str(HERE / ".voice/kokoro-v1.0.onnx"), str(HERE / ".voice/voices-v1.0.bin"))
out = HERE / "public/voice"
out.mkdir(parents=True, exist_ok=True)
for name, seconds in timing:
    budget = seconds - LEAD_IN - GAP
    speed = args.speed
    samples, rate = kokoro.create(LINES[name], voice=args.voice, speed=speed, lang="en-us")
    length = len(samples) / rate
    if length > budget:
        speed = min(1.25, speed * length / budget)
        samples, rate = kokoro.create(LINES[name], voice=args.voice, speed=speed, lang="en-us")
        length = len(samples) / rate
    sf.write(out / f"{name}.wav", samples, rate)
    fit = "fits" if length <= budget else "TOO LONG: shorten this line"
    print(f"{name:8} {length:4.1f}s of {budget:4.1f}s  speed {speed:.2f}  {fit}  \"{LINES[name]}\"")
