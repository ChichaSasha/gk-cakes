// Alpha-only cleanup of an enclosed backdrop pocket. RGB is never modified.
// Usage: island in.png out.png x0 y0 x1 y1 lo hi maxChroma
import Foundation
import ImageIO
import CoreGraphics
import UniformTypeIdentifiers
let a=CommandLine.arguments
let s=CGImageSourceCreateWithURL(URL(fileURLWithPath:a[1]) as CFURL,nil)!;let c=CGImageSourceCreateImageAtIndex(s,0,nil)!
precondition(c.bitsPerPixel==32 && c.alphaInfo == .last)
var bytes=[UInt8](c.dataProvider!.data! as Data);let row=c.bytesPerRow
let x0=Int(a[3])!,y0=Int(a[4])!,x1=Int(a[5])!,y1=Int(a[6])!,lo=Int(a[7])!,hi=Int(a[8])!,maxChroma=Int(a[9])!
var cleared=0
for y in y0...y1{for x in x0...x1{let i=y*row+x*4;let r=Int(bytes[i]),g=Int(bytes[i+1]),b=Int(bytes[i+2]);if bytes[i+3]>0 && [r,g,b].allSatisfy({$0>=lo&&$0<=hi}) && [r,g,b].max()!-[r,g,b].min()! <= maxChroma{bytes[i+3]=0;cleared+=1}}}
let out=CGImage(width:c.width,height:c.height,bitsPerComponent:8,bitsPerPixel:32,bytesPerRow:row,space:c.colorSpace!,bitmapInfo:CGBitmapInfo(rawValue:CGImageAlphaInfo.last.rawValue),provider:CGDataProvider(data:Data(bytes) as CFData)!,decode:nil,shouldInterpolate:false,intent:.defaultIntent)!
let d=CGImageDestinationCreateWithURL(URL(fileURLWithPath:a[2]) as CFURL,UTType.png.identifier as CFString,1,nil)!;CGImageDestinationAddImage(d,out,nil);precondition(CGImageDestinationFinalize(d))
print("{\"cleared_pixels\":\(cleared),\"region\":[\(x0),\(y0),\(x1),\(y1)],\"rgb_range\":[\(lo),\(hi)],\"max_chroma\":\(maxChroma)}")
