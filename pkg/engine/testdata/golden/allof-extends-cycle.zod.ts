import { z } from "zod";

export type A = B & { extra?: string; };
export const ASchema: z.ZodType<A> = z.lazy(() => z.intersection(BSchema, z.object({ extra: z.string().optional(), })));

export type B = { a?: A; };
export const BSchema: z.ZodType<B> = z.lazy(() => z.object({ a: ASchema.optional(), }));

