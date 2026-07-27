#!/usr/bin/env python3
"""T-081b 雙面驗證的「拆分後」那一半：拆開之後，所有約束能同時滿足嗎？

前半（two_sided_proof.py）問的是「拆分前，單一值能不能同時滿足兩個用途」。
這一支問的是建設性的另一半：**拆分後，指定一組具體色值，讓每個約束都達標。**

用的是同一套判準（audit.py 第二版的公式）：
  文字／圖示前景 → WCAG 對比度（正文 4.5:1）
  非文字元素     → 明度差 ΔL*（>= 3.0 才算明確可見）
"""
import sys

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

def over(fg, bg, a):
    return tuple(round(f * a + b * (1 - a)) for f, b in zip(fg, bg))

def hexc(h):
    h = h.lstrip("#")
    if len(h) == 3:
        h = "".join(ch * 2 for ch in h)
    return tuple(int(h[i:i + 2], 16) for i in (0, 2, 4))

def dl(c, base):
    return abs(lstar(c) - lstar(base))

# ── 兩份主題 ────────────────────────────────────────────────────────────
# 深色：內建 office 的實際值（拆出的 7 槽用它們的預設值——像素不變）
DARK = {
    "bg": "#191c24", "card": "#242832", "text": "#e7e8ee",
    "danger": "#f0736b", "switch": "#34c759", "indigo": "#2c3350",
    "overlay": "#fff", "shadow": "#000",
    "on-danger": "#fff", "on-indigo": "#fff", "knob": "#fff",
    "on-backdrop": "#fff", "backdrop": "#000", "surface-sunken": "#000",
    "scrollbar-thumb": "#2c3350",
}
# 淺色：以「精靈村」的底色族為基礎，替 7 個新槽各自挑一個只服務自己那個用途的值。
# 這正是拆分帶來的自由：以前這些值被綁在一起，只能二選一。
LIGHT = {
    "bg": "#eef0dc", "card": "#fbfcf0", "text": "#33301f",
    "danger": "#b03a30", "switch": "#4f9d2f", "indigo": "#3c5f8a",
    "overlay": "#000",              # 疊層要在淺底上看得見 → 取黑
    "shadow": "#4a4636",            # 投影要讀得出是陰影 → 中深灰
    "on-danger": "#fff",            # 壓在深紅底上 → 白
    "on-indigo": "#fff",            # 壓在飽和藍底上 → 白
    "knob": "#fff",                 # 壓在綠色軌道上 → 白
    "on-backdrop": "#fff",          # 坐在深遮罩上 → 白
    "backdrop": "#2b2a1c",          # 遮罩要壓得住底下的內容 → 深
    "surface-sunken": "#8a8768",    # 下沉表面：夠淡，55% 時正文仍可讀
    "scrollbar-thumb": "#6b7f9c",   # 捲軸拇指：對頁底要看得見
}

def checks(T):
    c = {k: hexc(v) for k, v in T.items()}
    r = []
    # --color-overlay：只剩疊層一個用途。最常見的 6% 描邊要看得見。
    r.append(("疊層 overlay 6% 疊在卡片上", "ΔL*", dl(over(c["overlay"], c["card"], .06), c["card"]), 3.0, ">="))
    # 拆出去的四個前景
    r.append(("未讀數字 on-danger 壓在紅底", "對比", ratio(c["on-danger"], c["danger"]), 4.5, ">="))
    r.append(("徽章/送出鈕 on-indigo 壓在藍底", "對比", ratio(c["on-indigo"], c["indigo"]), 4.5, ">="))
    r.append(("開關滑塊 knob 在開啟軌道上", "ΔL*", dl(c["knob"], c["switch"]), 3.0, ">="))
    r.append(("燈箱關閉鈕 on-backdrop 在遮罩上", "對比", ratio(c["on-backdrop"], c["backdrop"]), 4.5, ">="))
    # --color-shadow：只剩投影。最淡的一檔 28% 仍要讀得出是陰影。
    sh = over(c["shadow"], c["bg"], .28)
    r.append(("投影 shadow 28% 疊在頁底", "ΔL*", dl(sh, c["bg"]), 3.0, ">="))
    # 下沉表面：最深的一檔 55% 上面的正文仍要可讀
    r.append(("正文壓在 sunken 55% 表面上", "對比", ratio(c["text"], over(c["surface-sunken"], c["card"], .55)), 4.5, ">="))
    # 捲軸拇指對頁底
    r.append(("捲軸拇指對頁底", "ΔL*", dl(c["scrollbar-thumb"], c["bg"]), 3.0, ">="))
    return r

fail = 0
for name, T in (("內建深色主題（7 個新槽＝預設值，像素不變）", DARK),
                ("淺色主題（每個新槽各自挑值——拆分帶來的自由）", LIGHT)):
    print(f"\n{'='*70}\n{name}\n{'='*70}")
    for label, kind, val, thr, op in checks(T):
        ok = val >= thr
        if not ok:
            fail += 1
        print(f"  {'✅' if ok else '❌'} {label:34s} {kind} {val:6.2f}  (門檻 {op}{thr})")

print(f"\n{'='*70}")
print("結論：" + ("✅ 兩份主題下所有約束同時滿足——拆分後不再需要二選一。"
                 if fail == 0 else f"❌ 仍有 {fail} 項不達標"))
sys.exit(1 if fail else 0)
