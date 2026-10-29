"""Makes the home page's fonts from Go's own TTFs: Latin subsets as WOFF, written beside this file.

    go mod download -json golang.org/x/image@v0.47.0   # its Dir holds font/gofont/ttfs
    python3 make.py <Dir>/font/gofont/ttfs

Go Sans draws "-" as wide as a minus sign, since Go code uses it as one, and in prose it reads as an en dash. Its soft
hyphen (U+00AD) is drawn as a true hyphen, so the copy faces map "-" to that glyph. Go Mono keeps its own.
"""

import sys
from pathlib import Path

from fontTools import subset
from fontTools.ttLib import TTFont

UNICODES = [*range(0x20, 0x7F), *range(0xA0, 0x100), 0x2013, 0x2014, 0x2018, 0x2019, 0x201C, 0x201D, 0x2026, *range(0x2190, 0x2194)]
FONTS = {'Go-Regular': 'go-regular', 'Go-Medium': 'go-medium', 'Go-Bold': 'go-bold', 'Go-Mono': 'go-mono', 'Go-Mono-Bold': 'go-mono-bold'}

here = Path(__file__).parent
options = subset.Options(layout_features=['*'], flavor='woff')
for src, out in FONTS.items():
    font = TTFont(Path(sys.argv[1]) / f'{src}.ttf')
    subsetter = subset.Subsetter(options)
    subsetter.populate(unicodes=UNICODES)
    subsetter.subset(font)
    if not src.startswith('Go-Mono'):
        for table in font['cmap'].tables:
            if 0x2D in table.cmap:
                table.cmap[0x2D] = 'uni00AD'
    subset.save_font(font, here / f'{out}.woff', options)
