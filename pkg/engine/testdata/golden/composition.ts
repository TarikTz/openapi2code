export interface Animal {
  name: string;
}

export interface Dog extends Animal {
  breed: string;
}

export type Pet = Dog | Animal;

export type StringOrNumber = string | number;

