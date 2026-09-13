export interface Account {
  id: string;
  nickname?: string | null;
  note: string | null;
  status: Status;
}

export type Status = "active" | "inactive" | "pending";

