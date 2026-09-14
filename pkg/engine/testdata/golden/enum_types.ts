export type Flagged = true | false;

export type Label = "work" | "personal";

export type Priority = 1 | 2 | 3;

export type Score = 1 | 2.5 | 3;

export interface Ticket {
  flagged?: Flagged;
  label?: Label;
  priority: Priority;
  relatedPriorities?: Priority[];
  score?: Score;
  severity?: 1 | 2 | 3;
}

