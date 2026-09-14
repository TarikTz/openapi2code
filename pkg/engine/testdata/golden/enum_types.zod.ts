import { z } from "zod";

export const FlaggedSchema = z.union([z.literal(true), z.literal(false)]);
export type Flagged = z.infer<typeof FlaggedSchema>;

export const LabelSchema = z.enum(["work", "personal"]);
export type Label = z.infer<typeof LabelSchema>;

export const PrioritySchema = z.union([z.literal(1), z.literal(2), z.literal(3)]);
export type Priority = z.infer<typeof PrioritySchema>;

export const ScoreSchema = z.union([z.literal(1), z.literal(2.5), z.literal(3)]);
export type Score = z.infer<typeof ScoreSchema>;

export const TicketSchema = z.object({ flagged: FlaggedSchema.optional(), label: LabelSchema.optional(), priority: PrioritySchema, relatedPriorities: z.array(PrioritySchema).optional(), score: ScoreSchema.optional(), severity: z.union([z.literal(1), z.literal(2), z.literal(3)]).optional(), });
export type Ticket = z.infer<typeof TicketSchema>;

