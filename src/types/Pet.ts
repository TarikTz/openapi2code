import type { Category } from "./Category";
import type { Tag } from "./Tag";

export interface Pet {
  category?: Category;
  id?: number;
  name?: string;
  photoUrls?: string[];
  status?: string;
  tags?: Tag[];
}
