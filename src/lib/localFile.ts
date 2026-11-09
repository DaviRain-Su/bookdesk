/** 优先走系统保存对话框。宿主不在时退回浏览器下载，文件仍然只在本机。 */
export async function saveLocalFile(
  native: () => Promise<string>,
  blob: Blob,
  downloadName: string,
): Promise<"saved" | "cancelled" | "downloaded"> {
  try {
    const path = await native();
    if (!path) return "cancelled";
    return "saved";
  } catch {
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = downloadName;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
    return "downloaded";
  }
}

export function errorText(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
