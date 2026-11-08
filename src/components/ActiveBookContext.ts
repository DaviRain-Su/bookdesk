import { createContext } from "react";

/// 当前画布上被选中的那本书。摘录卡用它来判断「这本书」还是「其他书」。
export const ActiveBookContext = createContext<string | null>(null);
