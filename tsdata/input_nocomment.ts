export enum Gender {
  Male = 'Male',
  Female = 'Female',
  Unisex = 'Unisex',
  UNRECOGNIZED = 'UNRECOGNIZED',
}

export const GenderFromJSON = (object: any) => {
  switch (object) {
    case 1:
    case 'Male':
      return Gender.Male;
    case 2:
    case 'Female':
      return Gender.Female;
    case 3:
    case 'Unisex':
      return Gender.Unisex;
    case -1:
    case 'UNRECOGNIZED':
    default:
      return Gender.UNRECOGNIZED;
  }
};
