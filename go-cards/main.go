package main

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"

	"github.com/fogleman/gg"
	"github.com/go-pdf/fpdf"
	"github.com/golang/freetype/truetype"
	qrcode "github.com/yeqown/go-qrcode/v2"
	"github.com/yeqown/go-qrcode/writer/standard"
	"golang.org/x/image/font"
)

//go:embed assets/chessreel-logo-square.png
//go:embed assets/chessreel-logo-nobg.png
//go:embed assets/bernting-logo-square.png
//go:embed assets/bernting-logo-nobg.png
//go:embed assets/kendev-logo-square.png
//go:embed assets/kendev-logo-nobg.png
//go:embed assets/Fredoka-Regular.ttf
//go:embed assets/Inter-Black.ttf
//go:embed assets/Inter-Regular.ttf
//go:embed assets/IBMPlexMono-Bold.ttf
//go:embed assets/IBMPlexMono-Regular.ttf
var assets embed.FS

// Layout constants (mm) — matches the JS version exactly.
const (
	trimW = 90.0
	trimH = 55.0
	bleed = 3.0
	pageW = trimW + 2*bleed // 96mm
	pageH = trimH + 2*bleed // 61mm

	qrSizeMM     = 46.0
	urlTextHeight = 2.0
	contentHeight = qrSizeMM + urlTextHeight   // 48mm
	qrXInTrim     = (trimW - qrSizeMM) / 2    // 22mm
	qrYInTrim     = (trimH - contentHeight) / 2 // 3.5mm
	textXInTrim   = qrXInTrim                   // align with QR left edge
	textYInTrim   = qrYInTrim + qrSizeMM + 1    // 50.5mm

	pxPerMM = 20                   // 508 DPI — print quality
	canvasW = int(trimW * pxPerMM) // 1800
	canvasH = int(trimH * pxPerMM) // 1100

	// Logo sizing: 40% of the QR code, matching JS imageSize: 0.4.
	logoFraction = 0.4
	// Margin around logo (in QR image pixels) matching JS margin: 4 at 400px viewBox.
	// The QR is rendered at qrBlockWidth=20 per module. For a 33-module QR that's 660px.
	// JS uses margin=4 in a 400px viewBox → 4/400 = 1% → 660*0.01 ≈ 7px.
	logoMarginFrac = 4.0 / 400.0
)

// Dark card colors.
var (
	darkBase   = color.NRGBA{R: 18, G: 14, B: 42, A: 255} // #120e2a
	darkSquare = color.NRGBA{R: 30, G: 25, B: 58, A: 255} // #1e193a
)

type personalInfo struct {
	FullName string
	Title    string
	Email    string
	Phone    string
	Website  string // display text (no https://)
}

// personalStyle holds per-brand colors, fonts, and logos for personal cards.
type personalStyle struct {
	// Dark variant colors.
	BgColor      color.Color // dark card background
	NameColor    color.Color // dark card name
	TitleColor   color.Color // dark card title
	AccentColor  color.Color // dark card accent line
	ContactColor color.Color // dark card contact text
	// Light variant colors.
	LightNameColor    color.Color
	LightTitleColor   color.Color
	LightContactColor color.Color
	// Fonts and logos.
	BoldFont    []byte
	RegularFont []byte
	LogoLight   image.Image // logo for light cards (opaque bg)
	LogoDark    image.Image // logo for dark cards (transparent bg)
}

type cardConfig struct {
	Name     string
	URL      string
	Dark     bool
	Personal *personalInfo
	Style    *personalStyle // set for personal cards
}

var berntingInfo = &personalInfo{
	FullName: "William Bernting",
	Title:    "Enmansbyrå & Frilanskonsult",
	Email:    "william@bernting.se",
	Phone:    "+46 706 67 60 47",
	Website:  "william.bernting.se",
}

var kendevInfo = &personalInfo{
	FullName: "William Bernting",
	Title:    "KenDev AB",
	Email:    "william@kendev.se",
	Phone:    "+46 706 67 60 47",
	Website:  "kendev.se",
}

var henrikInfo = &personalInfo{
	FullName: "Henrik Ståhl",
	Title:    "Worst PO Ever",
	Email:    "henrik@maythecode.com",
	Phone:    "+46 73 352 53 55",
	Website:  "linkedin.com/in/henrikstahl",
}

// cards is populated in main() after styles are built.
var cards []cardConfig

func mustReadFile(name string) []byte {
	data, err := assets.ReadFile(name)
	if err != nil {
		panic(fmt.Errorf("read %s: %w", name, err))
	}
	return data
}

func main() {
	if err := os.MkdirAll("output", 0o755); err != nil {
		panic(err)
	}

	chessreelLogoLight := loadEmbeddedPNG("assets/chessreel-logo-square.png")
	chessreelLogoDark := loadEmbeddedPNG("assets/chessreel-logo-nobg.png")
	fontBytes := mustReadFile("assets/Fredoka-Regular.ttf")

	// Bernting style: Inter fonts, purple accent, dark blue-gray palette.
	berntingStyle := &personalStyle{
		BgColor:           color.NRGBA{R: 10, G: 10, B: 15, A: 255},                // #0a0a0f
		NameColor:         color.NRGBA{R: 0xe8, G: 0xe8, B: 0xf0, A: 255},          // #e8e8f0
		TitleColor:        color.NRGBA{R: 0x9b, G: 0x4d, B: 0xca, A: 255},          // #9b4dca
		AccentColor:       color.NRGBA{R: 0x9b, G: 0x4d, B: 0xca, A: 255},          // #9b4dca
		ContactColor:      color.NRGBA{R: 0x88, G: 0x88, B: 0xa0, A: 255},          // #8888a0
		LightNameColor:    color.NRGBA{R: 0x13, G: 0x13, B: 0x1a, A: 255},          // #13131a
		LightTitleColor:   color.NRGBA{R: 0x9b, G: 0x4d, B: 0xca, A: 255},          // #9b4dca
		LightContactColor: color.NRGBA{R: 0x55, G: 0x55, B: 0x6a, A: 255},          // #55556a
		BoldFont:          mustReadFile("assets/Inter-Black.ttf"),
		RegularFont:       mustReadFile("assets/Inter-Regular.ttf"),
		LogoLight:         loadEmbeddedPNG("assets/bernting-logo-square.png"),
		LogoDark:          loadEmbeddedPNG("assets/bernting-logo-nobg.png"),
	}

	// KenDev style: IBM Plex Mono, brutalist, purple accent on black/white.
	kendevStyle := &personalStyle{
		BgColor:           color.NRGBA{R: 0, G: 0, B: 0, A: 255},          // #000000
		NameColor:         color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 255}, // #ffffff
		TitleColor:        color.NRGBA{R: 0x88, G: 0x3a, B: 0xea, A: 255}, // #883aea
		AccentColor:       color.NRGBA{R: 0x88, G: 0x3a, B: 0xea, A: 255}, // #883aea
		ContactColor:      color.NRGBA{R: 0xaa, G: 0xaa, B: 0xaa, A: 255}, // #aaaaaa
		LightNameColor:    color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 255}, // #000000
		LightTitleColor:   color.NRGBA{R: 0x88, G: 0x3a, B: 0xea, A: 255}, // #883aea
		LightContactColor: color.NRGBA{R: 0x55, G: 0x55, B: 0x55, A: 255}, // #555555
		BoldFont:          mustReadFile("assets/IBMPlexMono-Bold.ttf"),
		RegularFont:       mustReadFile("assets/IBMPlexMono-Regular.ttf"),
		LogoLight:         loadEmbeddedPNG("assets/kendev-logo-square.png"),
		LogoDark:          loadEmbeddedPNG("assets/kendev-logo-nobg.png"),
	}

	// Henrik style: IBM Plex Mono, maythecode.com purple palette.
	henrikStyle := &personalStyle{
		BgColor:           color.NRGBA{R: 0x1a, G: 0x0a, B: 0x2e, A: 255}, // #1a0a2e deep purple-black
		NameColor:         color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 255}, // #ffffff
		TitleColor:        color.NRGBA{R: 0x99, G: 0x77, B: 0xee, A: 255}, // #9977ee bright lilac
		AccentColor:       color.NRGBA{R: 0x66, G: 0x22, B: 0xbb, A: 255}, // #6622bb maythecode purple
		ContactColor:      color.NRGBA{R: 0xbb, G: 0xcc, B: 0xee, A: 255}, // #bbccee soft blue-white
		LightNameColor:    color.NRGBA{R: 0x1a, G: 0x0a, B: 0x2e, A: 255}, // #1a0a2e
		LightTitleColor:   color.NRGBA{R: 0x66, G: 0x22, B: 0xbb, A: 255}, // #6622bb
		LightContactColor: color.NRGBA{R: 0x55, G: 0x55, B: 0x66, A: 255}, // #555566
		BoldFont:          mustReadFile("assets/IBMPlexMono-Bold.ttf"),
		RegularFont:       mustReadFile("assets/IBMPlexMono-Regular.ttf"),
	}

	cards = []cardConfig{
		{Name: "chessreel", URL: "https://www.chessreel.com", Dark: false},
		{Name: "chessparty", URL: "https://www.chessreel.com/chessparty", Dark: false},
		{Name: "chessreel-dark", URL: "https://www.chessreel.com", Dark: true},
		{Name: "chessparty-dark", URL: "https://www.chessreel.com/chessparty", Dark: true},
		{Name: "bernting", URL: "https://william.bernting.se", Dark: false, Personal: berntingInfo, Style: berntingStyle},
		{Name: "bernting-dark", URL: "https://william.bernting.se", Dark: true, Personal: berntingInfo, Style: berntingStyle},
		{Name: "kendev", URL: "https://kendev.se", Dark: false, Personal: kendevInfo, Style: kendevStyle},
		{Name: "kendev-dark", URL: "https://kendev.se", Dark: true, Personal: kendevInfo, Style: kendevStyle},
		{Name: "henrik", URL: "https://www.linkedin.com/in/henrikstahl", Dark: false, Personal: henrikInfo, Style: henrikStyle},
		{Name: "henrik-dark", URL: "https://www.linkedin.com/in/henrikstahl", Dark: true, Personal: henrikInfo, Style: henrikStyle},
	}

	for _, card := range cards {
		fmt.Printf("Generating %s ...\n", card.Name)

		qrImg := renderQR(card)

		var cardImg image.Image
		if card.Personal != nil {
			cardImg = drawPersonalCard(card, qrImg)
		} else {
			logo := chessreelLogoLight
			if card.Dark {
				logo = chessreelLogoDark
			}
			cardImg = drawCard(card, qrImg, logo, fontBytes)
		}

		pngPath := fmt.Sprintf("output/%s.png", card.Name)
		savePNG(cardImg, pngPath)

		pdfPath := fmt.Sprintf("output/%s.pdf", card.Name)
		generatePDF(card, pngPath, pdfPath)

		fmt.Printf("  → %s, %s\n", pngPath, pdfPath)
	}
}

// nopCloser wraps an io.Writer to satisfy io.WriteCloser.
type nopCloser struct{ *bytes.Buffer }

func (nopCloser) Close() error { return nil }

// renderQR generates a QR code image WITHOUT the logo.
// The logo is composited separately on the card canvas for full control.
func renderQR(card cardConfig) image.Image {
	opts := []standard.ImageOption{
		standard.WithQRWidth(20),
		standard.WithBuiltinImageEncoder(standard.PNG_FORMAT),
		standard.WithBorderWidth(0),
	}

	if card.Dark {
		opts = append(opts,
			standard.WithFgColorRGBHex("#ffffff"),
			standard.WithBgTransparent(),
		)
	} else {
		opts = append(opts,
			standard.WithFgColorRGBHex("#000000"),
			standard.WithBgColorRGBHex("#ffffff"),
		)
	}

	qrc, err := qrcode.NewWith(card.URL,
		qrcode.WithErrorCorrectionLevel(qrcode.ErrorCorrectionHighest),
		qrcode.WithEncodingMode(qrcode.EncModeByte),
	)
	if err != nil {
		panic(fmt.Errorf("new qrcode: %w", err))
	}

	buf := bytes.NewBuffer(nil)
	w := standard.NewWithWriter(nopCloser{buf}, opts...)

	if err = qrc.Save(w); err != nil {
		panic(fmt.Errorf("save qrcode: %w", err))
	}

	img, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		panic(fmt.Errorf("decode qr png: %w", err))
	}
	return img
}

// drawCard composites the full business card image (background + QR + logo + text).
// This single image is both the preview PNG and what gets embedded in the PDF.
func drawCard(card cardConfig, qrImg, logo image.Image, fontBytes []byte) image.Image {
	dc := gg.NewContext(canvasW, canvasH)

	if card.Dark {
		drawDarkBackground(dc)
	} else {
		dc.SetColor(color.White)
		dc.Clear()
	}

	// Snapshot the background before drawing the QR — used to repaint
	// the logo zone on dark cards so the checkerboard shows through.
	// Must copy because dc.Image() returns a mutable reference.
	var bgSnapshot *image.RGBA
	if card.Dark {
		src := dc.Image().(*image.RGBA)
		bgSnapshot = image.NewRGBA(src.Bounds())
		copy(bgSnapshot.Pix, src.Pix)
	}

	// QR code position and size in pixels.
	qrPx := qrSizeMM * pxPerMM // 920px
	qrX := qrXInTrim * pxPerMM // 440px
	qrY := qrYInTrim * pxPerMM // 70px

	// Scale and draw the QR code.
	qrBounds := qrImg.Bounds()
	scaleX := qrPx / float64(qrBounds.Dx())
	scaleY := qrPx / float64(qrBounds.Dy())

	dc.Push()
	dc.Translate(qrX, qrY)
	dc.Scale(scaleX, scaleY)
	dc.DrawImage(qrImg, 0, 0)
	dc.Pop()

	// Calculate logo size and position (centered in the QR area).
	logoSizePx := qrPx * logoFraction     // 40% of QR
	marginPx := qrPx * logoMarginFrac     // margin around logo
	clearSizePx := logoSizePx + 2*marginPx // total cleared area
	clearX := qrX + (qrPx-clearSizePx)/2
	clearY := qrY + (qrPx-clearSizePx)/2
	logoX := qrX + (qrPx-logoSizePx)/2
	logoY := qrY + (qrPx-logoSizePx)/2

	if card.Dark {
		// Restore the dark background in the logo zone — this erases
		// the white QR dots so the checkerboard shows through.
		cx := int(clearX)
		cy := int(clearY)
		cw := int(clearSizePx)
		ch := int(clearSizePx)
		clearRect := image.Rect(cx, cy, cx+cw, cy+ch)
		draw.Draw(dc.Image().(*image.RGBA), clearRect, bgSnapshot, image.Pt(cx, cy), draw.Src)
	} else {
		// For light cards: draw a white rect to hide QR dots behind the logo.
		dc.SetColor(color.White)
		dc.DrawRectangle(clearX, clearY, clearSizePx, clearSizePx)
		dc.Fill()
	}

	// Draw the logo scaled to logoSizePx × logoSizePx.
	logoBounds := logo.Bounds()
	logoScaleX := logoSizePx / float64(logoBounds.Dx())
	logoScaleY := logoSizePx / float64(logoBounds.Dy())

	dc.Push()
	dc.Translate(logoX, logoY)
	dc.Scale(logoScaleX, logoScaleY)
	dc.DrawImage(logo, 0, 0)
	dc.Pop()

	// Draw URL text using Fredoka font.
	textX := textXInTrim * pxPerMM
	textY := textYInTrim * pxPerMM
	fontSize := 4.5 * 0.3528 * pxPerMM // 4.5pt → mm → px

	ttFont, err := truetype.Parse(fontBytes)
	if err != nil {
		panic(fmt.Errorf("parse font: %w", err))
	}
	face := truetype.NewFace(ttFont, &truetype.Options{
		Size:    fontSize,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	dc.SetFontFace(face)

	if card.Dark {
		dc.SetColor(color.NRGBA{R: 180, G: 180, B: 210, A: 255})
	} else {
		dc.SetColor(color.NRGBA{R: 153, G: 153, B: 153, A: 255})
	}
	dc.DrawStringAnchored(card.URL, textX, textY, 0, 1)

	return dc.Image()
}

// drawPersonalCard composites a two-column personal business card:
// contact details on the left, QR code on the right, with logo in the QR center.
func drawPersonalCard(card cardConfig, qrImg image.Image) image.Image {
	dc := gg.NewContext(canvasW, canvasH)
	sty := card.Style

	// Colors: dark variant uses style colors; light uses explicit light colors.
	var nameColor, titleColor, contactColor color.Color
	if card.Dark {
		dc.SetColor(sty.BgColor)
		dc.Clear()
		nameColor = sty.NameColor
		titleColor = sty.TitleColor
		contactColor = sty.ContactColor
	} else {
		dc.SetColor(color.White)
		dc.Clear()
		nameColor = sty.LightNameColor
		titleColor = sty.LightTitleColor
		contactColor = sty.LightContactColor
	}

	// Parse fonts.
	boldFont, err := truetype.Parse(sty.BoldFont)
	if err != nil {
		panic(fmt.Errorf("parse bold font: %w", err))
	}
	regularFont, err := truetype.Parse(sty.RegularFont)
	if err != nil {
		panic(fmt.Errorf("parse regular font: %w", err))
	}

	// Font sizes in px (converted from pt via mm).
	nameSizePx := 78.0  // ~11pt
	titleSizePx := 46.0 // ~6.5pt
	infoSizePx := 42.0  // ~6pt

	nameFace := truetype.NewFace(boldFont, &truetype.Options{
		Size: nameSizePx, DPI: 72, Hinting: font.HintingFull,
	})
	titleFace := truetype.NewFace(regularFont, &truetype.Options{
		Size: titleSizePx, DPI: 72, Hinting: font.HintingFull,
	})
	infoFace := truetype.NewFace(regularFont, &truetype.Options{
		Size: infoSizePx, DPI: 72, Hinting: font.HintingFull,
	})

	leftX := 7.0 * pxPerMM // 7mm left margin

	// Name
	dc.SetFontFace(nameFace)
	dc.SetColor(nameColor)
	dc.DrawString(card.Personal.FullName, leftX, 14.0*pxPerMM)

	// Title
	dc.SetFontFace(titleFace)
	dc.SetColor(titleColor)
	dc.DrawString(card.Personal.Title, leftX, 20.0*pxPerMM)

	// Accent line
	dc.SetColor(sty.AccentColor)
	lineY := 24.0 * pxPerMM
	dc.SetLineWidth(0.5 * pxPerMM) // 0.5mm thick
	dc.DrawLine(leftX, lineY, leftX+20.0*pxPerMM, lineY)
	dc.Stroke()

	// Contact details
	dc.SetFontFace(infoFace)
	dc.SetColor(contactColor)
	dc.DrawString(card.Personal.Email, leftX, 29.0*pxPerMM)
	dc.DrawString(card.Personal.Phone, leftX, 34.0*pxPerMM)
	dc.DrawString(card.Personal.Website, leftX, 39.0*pxPerMM)

	// QR code: 28×28mm, right-aligned with 7mm margin (same as left), vertically centered.
	qrSizePersonalMM := 28.0
	qrSizePersonal := qrSizePersonalMM * pxPerMM // 560px
	qrX := (trimW - leftX/pxPerMM - qrSizePersonalMM) * pxPerMM // 55mm
	qrY := (trimH - qrSizePersonalMM) / 2 * pxPerMM             // 13.5mm

	// Snapshot bg before drawing QR (for dark card logo compositing).
	var bgSnapshot *image.RGBA
	if card.Dark {
		src := dc.Image().(*image.RGBA)
		bgSnapshot = image.NewRGBA(src.Bounds())
		copy(bgSnapshot.Pix, src.Pix)
	}

	qrBounds := qrImg.Bounds()
	scaleX := float64(qrSizePersonal) / float64(qrBounds.Dx())
	scaleY := float64(qrSizePersonal) / float64(qrBounds.Dy())

	dc.Push()
	dc.Translate(qrX, qrY)
	dc.Scale(scaleX, scaleY)
	dc.DrawImage(qrImg, 0, 0)
	dc.Pop()

	// Composite logo in the center of the QR code (if available).
	logo := sty.LogoLight
	if card.Dark {
		logo = sty.LogoDark
	}

	if logo != nil {
		qrPx := float64(qrSizePersonal)
		logoSizePx := qrPx * logoFraction     // 40% of QR
		marginPx := qrPx * logoMarginFrac     // margin around logo
		clearSizePx := logoSizePx + 2*marginPx // total cleared area
		clearX := qrX + (qrPx-clearSizePx)/2
		clearY := qrY + (qrPx-clearSizePx)/2
		logoX := qrX + (qrPx-logoSizePx)/2
		logoY := qrY + (qrPx-logoSizePx)/2

		if card.Dark {
			// Restore background in logo zone so bg shows through.
			cx := int(clearX)
			cy := int(clearY)
			cw := int(clearSizePx)
			ch := int(clearSizePx)
			clearRect := image.Rect(cx, cy, cx+cw, cy+ch)
			draw.Draw(dc.Image().(*image.RGBA), clearRect, bgSnapshot, image.Pt(cx, cy), draw.Src)
		} else {
			// White rect to hide QR dots behind logo.
			dc.SetColor(color.White)
			dc.DrawRectangle(clearX, clearY, clearSizePx, clearSizePx)
			dc.Fill()
		}

		// Draw the logo scaled to fit.
		logoBounds := logo.Bounds()
		logoScaleX := logoSizePx / float64(logoBounds.Dx())
		logoScaleY := logoSizePx / float64(logoBounds.Dy())

		dc.Push()
		dc.Translate(logoX, logoY)
		dc.Scale(logoScaleX, logoScaleY)
		dc.DrawImage(logo, 0, 0)
		dc.Pop()
	}

	return dc.Image()
}

// drawDarkBackground renders the checkerboard + radial gradient.
func drawDarkBackground(dc *gg.Context) {
	w := float64(canvasW)
	h := float64(canvasH)

	// 1. Fill base color.
	dc.SetColor(darkBase)
	dc.Clear()

	// 2. Checkerboard — 8 squares across trim width.
	sqPx := (trimW / 8) * pxPerMM // 225px
	cols := int(math.Ceil(w / sqPx))
	rows := int(math.Ceil(h / sqPx))
	dc.SetColor(darkSquare)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			if (r+c)%2 == 0 {
				dc.DrawRectangle(float64(c)*sqPx, float64(r)*sqPx, sqPx, sqPx)
				dc.Fill()
			}
		}
	}

	// 3. Radial gradient overlay.
	cx := w / 2
	cy := h / 2
	radius := math.Max(w, h) * 0.7

	grad := gg.NewRadialGradient(cx, cy, 0, cx, cy, radius)
	grad.AddColorStop(0, color.NRGBA{R: 30, G: 20, B: 70, A: 89}) // 0.35 * 255 ≈ 89
	grad.AddColorStop(1, color.NRGBA{R: 0, G: 0, B: 0, A: 0})
	dc.SetFillStyle(grad)
	dc.DrawRectangle(0, 0, w, h)
	dc.Fill()
}

// generatePDF wraps the card PNG into a print-ready PDF.
// The PDF is just a carrier for the exact same pixels — no separate text rendering.
func generatePDF(card cardConfig, pngPath, pdfPath string) {
	pdf := fpdf.NewCustom(&fpdf.InitType{
		UnitStr: "mm",
		Size:    fpdf.SizeType{Wd: pageW, Ht: pageH},
	})
	pdf.SetMargins(0, 0, 0)
	pdf.SetAutoPageBreak(false, 0)
	pdf.AddPage()

	// Fill entire page with bleed color.
	if card.Dark && card.Style != nil {
		// Use the personal card's background color for bleed.
		r, g, b, _ := card.Style.BgColor.RGBA()
		pdf.SetFillColor(int(r>>8), int(g>>8), int(b>>8))
	} else if card.Dark {
		pdf.SetFillColor(18, 14, 42) // #120e2a
	} else {
		pdf.SetFillColor(255, 255, 255)
	}
	pdf.Rect(0, 0, pageW, pageH, "F")

	// Place card PNG at bleed offset, covering the trim area.
	opts := fpdf.ImageOptions{ImageType: "PNG", ReadDpi: false}
	pdf.ImageOptions(pngPath, bleed, bleed, trimW, trimH, false, opts, 0, "")

	if err := pdf.OutputFileAndClose(pdfPath); err != nil {
		panic(fmt.Errorf("save pdf: %w", err))
	}
}

// loadEmbeddedPNG decodes a PNG from the embedded filesystem.
func loadEmbeddedPNG(path string) image.Image {
	data, err := assets.ReadFile(path)
	if err != nil {
		panic(fmt.Errorf("read embedded %s: %w", path, err))
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		panic(fmt.Errorf("decode %s: %w", path, err))
	}
	return img
}

// savePNG encodes an image to a PNG file.
func savePNG(img image.Image, path string) {
	bounds := img.Bounds()
	nrgba := image.NewNRGBA(bounds)
	draw.Draw(nrgba, bounds, img, bounds.Min, draw.Src)

	f, err := os.Create(path)
	if err != nil {
		panic(fmt.Errorf("create %s: %w", path, err))
	}
	defer f.Close()
	if err := png.Encode(f, nrgba); err != nil {
		panic(fmt.Errorf("encode %s: %w", path, err))
	}
}
