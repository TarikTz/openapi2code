import { z } from "zod";

export type Node = { id: string; next?: Node; };
export const NodeSchema: z.ZodType<Node> = z.lazy(() => z.object({ id: z.string(), next: NodeSchema.optional(), }));

export const NamedNodeSchema = z.intersection(NodeSchema, z.object({ name: z.string(), }));
export type NamedNode = z.infer<typeof NamedNodeSchema>;

