import { z } from "zod";

export const CategorySchema = z.object({ id: z.number().optional(), name: z.string(), });
export type Category = z.infer<typeof CategorySchema>;

export const ErrorSchema = z.object({ code: z.number(), message: z.string(), });
export type Error = z.infer<typeof ErrorSchema>;

export const TagSchema = z.object({ id: z.number().optional(), name: z.string(), });
export type Tag = z.infer<typeof TagSchema>;

export const PetSchema = z.object({ category: CategorySchema.optional(), id: z.number().optional(), name: z.string(), photoUrls: z.array(z.string()), status: z.enum(["available", "pending", "sold"]).optional(), tags: z.array(TagSchema).optional(), });
export type Pet = z.infer<typeof PetSchema>;

