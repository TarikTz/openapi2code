import { z } from "zod";

export type Thread = { author: User; parent?: Thread; title: string; };
export const ThreadSchema: z.ZodType<Thread> = z.lazy(() => z.object({ author: UserSchema, parent: ThreadSchema.optional(), title: z.string(), }));

export const CommentSchema = z.object({ body: z.string(), thread: ThreadSchema, });
export type Comment = z.infer<typeof CommentSchema>;

export const UserSchema = z.object({ name: z.string(), });
export type User = z.infer<typeof UserSchema>;

