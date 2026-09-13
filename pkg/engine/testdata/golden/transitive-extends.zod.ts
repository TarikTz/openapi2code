import { z } from "zod";

export type Base = { id: string; self?: Base; };
export const BaseSchema: z.ZodType<Base> = z.lazy(() => z.object({ id: z.string(), self: BaseSchema.optional(), }));

export const MidSchema = z.intersection(BaseSchema, z.object({ mid: z.string(), }));
export type Mid = z.infer<typeof MidSchema>;

export const LeafSchema = z.intersection(MidSchema, z.object({ leaf: z.string(), }));
export type Leaf = z.infer<typeof LeafSchema>;

