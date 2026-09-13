export interface Category {
  id?: number;
  name: string;
}

export interface Error {
  code: number;
  message: string;
}

export interface Pet {
  category?: Category;
  id?: number;
  name: string;
  nickname: string | null;
  photoUrls: string[];
  status?: "available" | "pending" | "sold";
  tags?: Tag[];
}

export interface Tag {
  id?: number;
  name: string;
}

