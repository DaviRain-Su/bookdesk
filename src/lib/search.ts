import type { Search } from "../mygo";

export type SearchHit = Awaited<ReturnType<typeof Search.query>>[number];
