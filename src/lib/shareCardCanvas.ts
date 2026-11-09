import {
  CARD_SANS,
  CARD_SERIF,
  type CardLayout,
  type Paint,
} from "./shareCard";

/** 按排版结果画到画布上。字体只写本机家族名，不发起网络请求。 */
export function drawShareCard(
  ctx: CanvasRenderingContext2D,
  layout: CardLayout,
  scale = 1,
): void {
  ctx.save();
  ctx.setTransform(scale, 0, 0, scale, 0, 0);
  ctx.clearRect(0, 0, layout.width, layout.height);
  for (const paint of layout.paints) drawPaint(ctx, paint);
  ctx.restore();
}

function drawPaint(ctx: CanvasRenderingContext2D, paint: Paint): void {
  if (paint.kind === "rect") {
    if (paint.fill) {
      ctx.fillStyle = paint.fill;
      ctx.fillRect(paint.x, paint.y, paint.w, paint.h);
    }
    if (paint.stroke) {
      ctx.strokeStyle = paint.stroke;
      ctx.lineWidth = paint.strokeWidth ?? 1;
      ctx.strokeRect(paint.x, paint.y, paint.w, paint.h);
    }
    return;
  }
  ctx.fillStyle = paint.fill;
  ctx.font = `${paint.size}px ${paint.font === "serif" ? CARD_SERIF : CARD_SANS}`;
  ctx.textBaseline = "alphabetic";
  if (paint.kind === "text") {
    ctx.textAlign = paint.align;
    ctx.fillText(paint.text, paint.x, paint.y);
    return;
  }
  ctx.textAlign = "left";
  const chars = Array.from(paint.text);
  chars.forEach((ch, index) => {
    const punct = "，。、；：！？".includes(ch);
    const x = paint.x + (punct ? paint.size * 0.42 : 0);
    const y = paint.y + (index + 1) * paint.size * paint.step - (punct ? paint.size * 0.15 : 0);
    ctx.fillText(ch, x, y);
  });
}

export function canvasForLayout(layout: CardLayout, scale = 2): HTMLCanvasElement {
  const canvas = document.createElement("canvas");
  canvas.width = Math.round(layout.width * scale);
  canvas.height = Math.round(layout.height * scale);
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("无法创建画布");
  drawShareCard(ctx, layout, scale);
  return canvas;
}

export function canvasToBlob(canvas: HTMLCanvasElement): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (!blob) {
        reject(new Error("无法生成 PNG"));
        return;
      }
      resolve(blob);
    }, "image/png");
  });
}

export function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const value = String(reader.result ?? "");
      const comma = value.indexOf(",");
      resolve(comma >= 0 ? value.slice(comma + 1) : value);
    };
    reader.onerror = () => reject(reader.error ?? new Error("无法读取图片"));
    reader.readAsDataURL(blob);
  });
}
