import { Config } from '@remotion/cli/config'

// Crisp 1080p60 H.264 for LinkedIn: PNG frames (no JPEG artefacts on text), CRF 16, the yuv420p every player takes.
Config.setEntryPoint('src/index.ts')
Config.setVideoImageFormat('png')
Config.setCodec('h264')
Config.setCrf(16)
Config.setPixelFormat('yuv420p')
