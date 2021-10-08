package tsdata

type Owner int

const (
	Manager    Owner = 1 // owner.manager.role
	Employee   Owner = 2 // owner.employee.role
	Contractor Owner = 3 // owner.contractor.role
)
