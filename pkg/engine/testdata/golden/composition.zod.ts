import { z } from "zod";

export const AnimalSchema = z.object({ name: z.string(), });
export type Animal = z.infer<typeof AnimalSchema>;

export const DogSchema = AnimalSchema.extend({ breed: z.string(), });
export type Dog = z.infer<typeof DogSchema>;

export const PetSchema = z.union([DogSchema, AnimalSchema]);
export type Pet = z.infer<typeof PetSchema>;

export const StringOrNumberSchema = z.union([z.string(), z.number()]);
export type StringOrNumber = z.infer<typeof StringOrNumberSchema>;

