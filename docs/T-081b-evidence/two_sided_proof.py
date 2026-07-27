#!/usr/bin/env python3
"""T-081b 雙面驗證：證明拆分前無解、拆分後有解。

owner 2026-07-27 於回覆卡 rc-800a7adb224f 選項一拍板的驗收標準。

兩個問題各自回答：
  (b-before) 拆分前：對三個身兼二職的色槽，是否存在**任何一個顏色**能同時滿足它的
             兩種用途？做法是掃過整個 RGB 空間（每軸步進 STEP），逐色檢查兩個條件。
             結論若為「零個顏色通過」，就證明了衝突是結構性的、不是調色沒調好。
  (a-after)  拆分後：每個新 token 各自只剩一個用途，給出一組具體色值使其達標。

判準沿用 T-4527 的 audit.py 第二版：
  * 文字／圖示前景 → WCAG 對比度（正文 4.5:1；大字與圖形元件 3:1）
  * 非文字元素（邊框、疊層、捲軸、表面）→ 明度差 ΔL*（<1.0 等於看不見；>=3.0 才明確）
公式逐行對齊 audit.py，不自創。
"""

STEP = 3  # RGB 每軸步進；86^3 ≈ 636k 色，足以涵蓋任何實務上的選色

# ── audit.py 第二版的公式（原樣搬過來，不改） ──────────────────────────
def _srgb(v):
    s = v / 255
    return s / 12.92 if s <= 0.03928 else ((s + 0.055) / 1.055) ** 2.4

def lum(c):
    r, g, b = (_srgb(x) for x in c)
    return 0.2126 * r + 0.7152 * g + 0.0722 * b

def ratio(a, b):
    la, lb = lum(a), lum(b)
    return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)

def lstar(c):
    y = lum(c)
    return 116 * (y ** (1 / 3)) - 16 if y > 0.008856 else 903.3 * y

def over(fg, bg, alpha):
    """fg 以 alpha 疊在 bg 上（color-mix(fg N%, transparent) 的實際合成）。"""
    return tuple(round(f * alpha + b * (1 - alpha)) for f, b in zip(fg, bg))

def hexc(h):
    h = h.lstrip("#")
    return tuple(int(h[i:i + 2], 16) for i in (0, 2, 4))

def dl(c, base):
    return abs(lstar(c) - lstar(base))

# ── 場景：一份具代表性的淺色主題（取自「精靈村」實際填的底色族） ──────
# 只取「不屬於本票爭議」的槽當固定背景，這樣衝突就完全歸因於被測色槽本身。
CARD    = hexc("#fbfcf0")   # 卡片底
BG      = hexc("#eef0dc")   # 頁底
DANGER  = hexc("#b03a30")   # 未讀徽章的紅底
SWITCH  = hexc("#4f9d2f")   # 開關開啟時的軌道綠
TEXT    = hexc("#33301f")   # 正文字

# 為了對照，同樣跑一次內建深色主題，證明「深色主題兩邊剛好都成立」——
# 這正是問題直到淺色主題才爆的原因。
DARK = dict(CARD=hexc("#242832"), BG=hexc("#191c24"), DANGER=hexc("#f0736b"),
            SWITCH=hexc("#34c759"), TEXT=hexc("#e7e8ee"))


def jobs_overlay(c, card, bg, danger, switch, text):
    """--color-overlay 的兩種用途。"""
    # 用途 A：半透明疊層基色。最常見的是 6% 的描邊/hover——必須看得見。
    a_ok = dl(over(c, card, 0.06), card) >= 1.0 and dl(over(c, bg, 0.06), bg) >= 1.0
    # 用途 B：實色前景。壓在紅色未讀徽章上的數字（正文級 4.5:1），
    #          以及開關滑塊要在軌道上看得出來（圖形元件 3:1）。
    b_ok = ratio(c, danger) >= 4.5 and ratio(c, switch) >= 3.0
    return a_ok, b_ok


def jobs_shadow(c, card, bg, danger, switch, text):
    """--color-shadow 的兩種用途。

    關鍵在於「同一個值要撐過整個使用百分比範圍」：
    投影用 28~50%，下沉表面用 4~55%。約束要取各自的最壞情況，
    否則只挑中間的百分比看，衝突不會顯現。
    """
    # 用途 A：box-shadow。取實際用到的最淡一檔 28%，仍必須讀得出是陰影（明顯更暗）。
    sh = over(c, bg, 0.28)
    a_ok = dl(sh, bg) >= 3.0 and lstar(sh) < lstar(bg)
    # 用途 B：下沉表面。取實際用到的最深一檔 55%，上面的正文仍要可讀。
    #   （不把「最淡的 4% 要看得見」列進來：那一檔連現行內建深色主題自己都不滿足，
    #    是既有的裝飾性瑕疵，拿它當門檻會讓「改動前」因為錯的理由而紅。）
    b_ok = ratio(text, over(c, card, 0.55)) >= 4.5
    return a_ok, b_ok


def jobs_indigo(c, card, bg, danger, switch, text):
    """--color-indigo 的兩種用途。

    ⚠️ 誠實說明：這一組的兩個約束在無障礙門檻上**並不互斥**（實測見報告）。
    票面說的「頁籤底需要淡」是美感取捨、不是可量測的門檻，這裡不硬湊一個
    會產出「無解」結論的假約束。拆它的理由是關注點分離，不是不可能。
    """
    # 用途 A：實色動作鈕／active 底，上面要放前景字（4.5:1）。
    #          前景取該主題會用的最佳選擇——白或黑，取較有利者。
    a_ok = max(ratio(hexc("#ffffff"), c), ratio(hexc("#000000"), c)) >= 4.5
    # 用途 B：捲軸拇指，對頁底要看得見（圖形元件，ΔL* >= 3.0）。
    b_ok = dl(c, bg) >= 3.0
    return a_ok, b_ok


CASES = [
    ("--color-overlay", jobs_overlay,
     "半透明疊層基色", "壓在紅底／開關軌道上的實色前景"),
    ("--color-shadow", jobs_shadow,
     "box-shadow 投影", "下沉一層的表面底（上面還要放正文）"),
    ("--color-indigo", jobs_indigo,
     "實色動作底（上面放字）", "捲軸拇指（對頁底要看得見）"),
]


def sweep(fn, card, bg, danger, switch, text):
    both = a_only = b_only = 0
    example_both = None
    rng = range(0, 256, STEP)
    for r in rng:
        for g in rng:
            for b in rng:
                c = (r, g, b)
                a_ok, b_ok = fn(c, card, bg, danger, switch, text)
                if a_ok and b_ok:
                    both += 1
                    if example_both is None:
                        example_both = c
                elif a_ok:
                    a_only += 1
                elif b_ok:
                    b_only += 1
    return both, a_only, b_only, example_both


def run(label, card, bg, danger, switch, text):
    print(f"\n{'='*72}\n{label}\n{'='*72}")
    verdict = {}
    for name, fn, ja, jb in CASES:
        both, a_only, b_only, ex = sweep(fn, card, bg, danger, switch, text)
        total = len(range(0, 256, STEP)) ** 3
        print(f"\n{name}")
        print(f"  用途 A = {ja}")
        print(f"  用途 B = {jb}")
        print(f"  掃過 {total} 色：只滿足 A {a_only}、只滿足 B {b_only}、"
              f"**同時滿足 {both}**")
        if both and ex is not None:
            print(f"  → 兩用途相容，例如 #{ex[0]:02x}{ex[1]:02x}{ex[2]:02x}")
        else:
            print(f"  → 🔴 不存在任何顏色能同時滿足兩者：這個 token 在此主題下無解")
        verdict[name] = both
    return verdict


if __name__ == "__main__":
    light = run("淺色主題（精靈村底色族）— 這是使用者實際會踩到的情境",
                CARD, BG, DANGER, SWITCH, TEXT)
    dark = run("內建深色主題 — 對照組：說明為何問題直到淺色主題才浮現",
               DARK["CARD"], DARK["BG"], DARK["DANGER"], DARK["SWITCH"], DARK["TEXT"])

    print(f"\n{'='*72}\n總結\n{'='*72}")
    for name, _, _, _ in CASES:
        print(f"  {name:22s} 淺色主題可行色數 {light[name]:>6d}   "
              f"深色主題可行色數 {dark[name]:>6d}")
    unsolvable = [n for n in light if light[n] == 0]
    print(f"\n淺色主題下無解的色槽：{len(unsolvable)} / {len(CASES)} → {unsolvable}")
    print("拆分後每個 token 只承擔一個用途，上述兩欄約束不再需要由同一個值同時滿足。")
