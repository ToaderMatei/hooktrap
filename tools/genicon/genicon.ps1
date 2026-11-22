# Generates installer\hooktrap.ico and docs\icon.png.
# Symbol: a red fishing-hook with an inbound signal dot ("capture the hook"),
# on a dark rounded square — matching the m68k red-terminal theme.
Add-Type -AssemblyName System.Drawing
$root  = Resolve-Path (Join-Path $PSScriptRoot "..\..")
$icoOut = Join-Path $root "installer\hooktrap.ico"
$pngOut = Join-Path $root "docs\icon.png"
$sizes = 16,32,48,64,128,256
$pngs = @()

function DrawIcon([System.Drawing.Graphics]$g, [int]$s) {
  $g.SmoothingMode = 'AntiAlias'
  $g.Clear([System.Drawing.Color]::Transparent)
  # rounded dark background
  $bg  = New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::FromArgb(255,11,7,8))
  $rad = [math]::Max(2,[int]($s*0.18)); $d = $rad*2
  $bgp = New-Object System.Drawing.Drawing2D.GraphicsPath
  $bgp.AddArc(0,0,$d,$d,180,90); $bgp.AddArc($s-$d,0,$d,$d,270,90)
  $bgp.AddArc($s-$d,$s-$d,$d,$d,0,90); $bgp.AddArc(0,$s-$d,$d,$d,90,90); $bgp.CloseFigure()
  $g.FillPath($bg,$bgp)
  $border = New-Object System.Drawing.Pen ([System.Drawing.Color]::FromArgb(110,255,43,58)), ([single]([math]::Max(1,$s/48)))
  $g.DrawPath($border,$bgp)

  # hook, drawn in a 0..1 coordinate space scaled by s
  $red = [System.Drawing.Color]::FromArgb(255,255,43,58)
  $pen = New-Object System.Drawing.Pen $red, ([single]($s*0.085))
  $pen.StartCap = 'Round'; $pen.EndCap = 'Round'; $pen.LineJoin = 'Round'
  function P([double]$x,[double]$y){ New-Object System.Drawing.PointF ([single]($x*$s)),([single]($y*$s)) }

  # eyelet ring at the top of the shaft
  $er = 0.085
  $g.DrawEllipse($pen, [single]((0.5-$er)*$s), [single]((0.14)*$s), [single]($er*2*$s), [single]($er*2*$s))

  # shaft + J-curve (the hook body) as a bezier path
  $hook = New-Object System.Drawing.Drawing2D.GraphicsPath
  $hook.AddLine((P 0.5 0.31), (P 0.5 0.60))
  # curve down and around to the left, then back up to the barb tip
  $hook.AddBezier((P 0.5 0.60), (P 0.5 0.80), (P 0.30 0.80), (P 0.30 0.62))
  $hook.AddBezier((P 0.30 0.62), (P 0.30 0.54), (P 0.36 0.52), (P 0.40 0.56))
  $g.DrawPath($pen, $hook)

  # inbound "signal" dot being caught (top-right), on-theme accent-2
  $dotB = New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::FromArgb(255,255,122,107))
  $dr = 0.075
  $g.FillEllipse($dotB, [single]((0.66)*$s), [single]((0.26)*$s), [single]($dr*2*$s), [single]($dr*2*$s))
}

foreach ($s in $sizes) {
  $bmp = New-Object System.Drawing.Bitmap $s,$s
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  DrawIcon $g $s
  $g.Dispose()
  $ms = New-Object System.IO.MemoryStream
  $bmp.Save($ms,[System.Drawing.Imaging.ImageFormat]::Png)
  $pngs += ,($ms.ToArray()); $bmp.Dispose()
}

# assemble ICO (PNG-compressed entries)
$out = New-Object System.IO.MemoryStream
$bw = New-Object System.IO.BinaryWriter $out
$bw.Write([uint16]0); $bw.Write([uint16]1); $bw.Write([uint16]$sizes.Count)
$offset = 6 + 16*$sizes.Count
for ($i=0;$i -lt $sizes.Count;$i++){
  $s=$sizes[$i]; $len=$pngs[$i].Length
  $dim = if($s -ge 256){0}else{$s}
  $bw.Write([byte]$dim); $bw.Write([byte]$dim); $bw.Write([byte]0); $bw.Write([byte]0)
  $bw.Write([uint16]1); $bw.Write([uint16]32); $bw.Write([uint32]$len); $bw.Write([uint32]$offset)
  $offset += $len
}
foreach($p in $pngs){ $bw.Write($p) }; $bw.Flush()
[System.IO.File]::WriteAllBytes($icoOut, $out.ToArray())
[System.IO.File]::WriteAllBytes($pngOut, $pngs[$sizes.Count-1])
Write-Host "wrote $icoOut ($((Get-Item $icoOut).Length) bytes) and $pngOut"
