/** Encoders by ffmpeg's name, as a viewer knows them. */
export const ENCODERS: Record<string, string> = {
  h264_nvenc: 'NVIDIA',
  h264_qsv: 'Intel Quick Sync',
  h264_amf: 'AMD',
  h264_videotoolbox: 'Apple VideoToolbox',
  h264_vaapi: 'VAAPI',
  libx264: 'Software',
}
