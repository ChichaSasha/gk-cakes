// Confirms every non-transparent cutout pixel has exactly the source RGB at the recorded crop offset.
import Foundation
import ImageIO
import CoreGraphics
func load(_ p:String)->CGImage{let s=CGImageSourceCreateWithURL(URL(fileURLWithPath:p) as CFURL,nil)!;return CGImageSourceCreateImageAtIndex(s,0,nil)!}
let a=CommandLine.arguments, src=load(a[1]), cut=load(a[2]), ox=Int(a[3])!, oy=Int(a[4])!
var s=[UInt8](repeating:0,count:src.width*src.height*4)
CGContext(data:&s,width:src.width,height:src.height,bitsPerComponent:8,bytesPerRow:src.width*4,space:CGColorSpace(name:CGColorSpace.sRGB)!,bitmapInfo:CGImageAlphaInfo.premultipliedLast.rawValue)!.draw(src,in:CGRect(x:0,y:0,width:src.width,height:src.height))
precondition(cut.bitsPerPixel==32 && cut.alphaInfo == .last, "unexpected cutout format \(cut.bitsPerPixel) \(cut.alphaInfo.rawValue)")
let d=cut.dataProvider!.data! as Data, row=cut.bytesPerRow
var checked=0, mismatch=0, transparent=0
d.withUnsafeBytes{(c:UnsafeRawBufferPointer) in
 for y in 0..<cut.height{for x in 0..<cut.width{let i=y*row+x*4;if c[i+3]==0{transparent+=1;continue};checked+=1;let j=((oy+y)*src.width+(ox+x))*4;if c[i] != s[j] || c[i+1] != s[j+1] || c[i+2] != s[j+2]{mismatch+=1}}}}
print("{\"checked_visible_pixels\":\(checked),\"rgb_mismatches\":\(mismatch),\"transparent_pixels\":\(transparent)}")
