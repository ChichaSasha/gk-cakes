// Local, non-generative subject extraction for GK-015 r09.
// Apple Vision foreground mask -> alpha channel applied to the ORIGINAL pixels (RGB unchanged),
// cropped to the subject's alpha bounds plus padding. Usage: swift extract.swift in.webp out.png out.json
import Foundation
import Vision
import ImageIO
import CoreVideo
import UniformTypeIdentifiers

let a = CommandLine.arguments
let inURL = URL(fileURLWithPath: a[1]), outURL = URL(fileURLWithPath: a[2]), jsonURL = URL(fileURLWithPath: a[3])
guard let src = CGImageSourceCreateWithURL(inURL as CFURL, nil),
      let cg = CGImageSourceCreateImageAtIndex(src, 0, nil) else { fatalError("cannot read input") }
let W = cg.width, H = cg.height

let request = VNGenerateForegroundInstanceMaskRequest()
let handler = VNImageRequestHandler(cgImage: cg, options: [:])
try handler.perform([request])
guard let obs = request.results?.first else { fatalError("no foreground subject found") }
let instances = obs.allInstances
let mask = try obs.generateScaledMaskForImage(forInstances: instances, from: handler)

CVPixelBufferLockBaseAddress(mask, .readOnly)
let mw = CVPixelBufferGetWidth(mask), mh = CVPixelBufferGetHeight(mask)
precondition(mw == W && mh == H, "mask size \(mw)x\(mh) != image \(W)x\(H)")
let mrow = CVPixelBufferGetBytesPerRow(mask)
let mbase = CVPixelBufferGetBaseAddress(mask)!

// Original pixels as RGBA8. The source is opaque, so premultiplied drawing leaves RGB exact.
let space = cg.colorSpace ?? CGColorSpace(name: CGColorSpace.sRGB)!
var rgba = [UInt8](repeating: 0, count: W * H * 4)
let ctx = CGContext(data: &rgba, width: W, height: H, bitsPerComponent: 8, bytesPerRow: W * 4,
                    space: space, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
ctx.draw(cg, in: CGRect(x: 0, y: 0, width: W, height: H))

var minX = W, minY = H, maxX = -1, maxY = -1, opaque = 0, partial = 0, nonOpaqueSource = 0
for y in 0..<H {
  let row = mbase.advanced(by: y * mrow).assumingMemoryBound(to: Float32.self)
  for x in 0..<W {
    let i = (y * W + x) * 4
    if rgba[i + 3] != 255 { nonOpaqueSource += 1 }
    let m = max(0, min(1, row[x]))
    let alpha = UInt8((m * 255).rounded())
    rgba[i + 3] = alpha
    if alpha > 0 { if alpha == 255 { opaque += 1 } else { partial += 1 } }
    if alpha > 8 { minX = min(minX, x); maxX = max(maxX, x); minY = min(minY, y); maxY = max(maxY, y) }
  }
}
CVPixelBufferUnlockBaseAddress(mask, .readOnly)

let pad = 24
let cx = max(0, minX - pad), cy = max(0, minY - pad)
let cw = min(W, maxX + pad + 1) - cx, ch = min(H, maxY + pad + 1) - cy
var crop = [UInt8](repeating: 0, count: cw * ch * 4)
for y in 0..<ch { for x in 0..<cw {
  let s = ((cy + y) * W + (cx + x)) * 4, d = (y * cw + x) * 4
  crop[d] = rgba[s]; crop[d + 1] = rgba[s + 1]; crop[d + 2] = rgba[s + 2]; crop[d + 3] = rgba[s + 3]
}}
// Straight (non-premultiplied) alpha so subject RGB values are written exactly.
let provider = CGDataProvider(data: Data(crop) as CFData)!
let out = CGImage(width: cw, height: ch, bitsPerComponent: 8, bitsPerPixel: 32, bytesPerRow: cw * 4, space: space,
                  bitmapInfo: CGBitmapInfo(rawValue: CGImageAlphaInfo.last.rawValue), provider: provider,
                  decode: nil, shouldInterpolate: false, intent: .defaultIntent)!
let dest = CGImageDestinationCreateWithURL(outURL as CFURL, UTType.png.identifier as CFString, 1, nil)!
CGImageDestinationAddImage(dest, out, nil)
precondition(CGImageDestinationFinalize(dest), "png write failed")

let report: [String: Any] = [
  "method": "Apple Vision VNGenerateForegroundInstanceMaskRequest (on-device segmentation, no generation); mask applied as alpha to original pixels",
  "source_size": [W, H], "instances": instances.count, "source_non_opaque_pixels": nonOpaqueSource,
  "subject_bbox_alpha_gt_8": [minX, minY, maxX, maxY], "crop_xywh": [cx, cy, cw, ch], "padding_px": pad,
  "opaque_pixels": opaque, "partial_alpha_pixels": partial, "colour_space": (space.name as String?) ?? "unknown"]
let json = try JSONSerialization.data(withJSONObject: report, options: [.prettyPrinted, .sortedKeys])
try json.write(to: jsonURL)
print(String(data: json, encoding: .utf8)!)
