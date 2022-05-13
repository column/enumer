export enum Owner {
  Manager = 'owner.manager.role',
  Employee = 'owner.employee.role',
  Contractor = 'owner.contractor.role',
  UNRECOGNIZED = 'UNRECOGNIZED',
}

export const OwnerFromJSON = (object: any) => {
  switch (object) {
    case 1:
    case 'owner.manager.role':
      return Owner.Manager;
    case 2:
    case 'owner.employee.role':
      return Owner.Employee;
    case 3:
    case 'owner.contractor.role':
      return Owner.Contractor;
    case -1:
    case 'UNRECOGNIZED':
    default:
      return Owner.UNRECOGNIZED;
  }
};
