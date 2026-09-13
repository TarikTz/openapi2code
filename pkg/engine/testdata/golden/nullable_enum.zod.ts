import { z } from "zod";

export const StatusSchema = z.enum(["active", "inactive", "pending"]);
export type Status = z.infer<typeof StatusSchema>;

export const AccountSchema = z.object({ id: z.string(), nickname: z.string().nullable().optional(), note: z.string().nullable(), status: StatusSchema, });
export type Account = z.infer<typeof AccountSchema>;

